// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Testapp is a small program that wires the auditlog SDK and emits sample audit
// events to stdout via a simple log exporter.
//
// Run from the auditlog module directory (uses testapp/dev_hmac_key.txt on disk when env is unset):
//
//	go run ./testapp
//
// Send to an OpenTelemetry Collector OTLP HTTP receiver (audit logs path `/auditlogs`) instead of stdout JSON:
//
//	go run ./testapp -otlp-endpoint http://localhost:4318
//
// An explicit path overrides the default (for example `http://localhost:4318/auditlogs`).
//
// For durability when export fails or the process restarts, add a file-backed store:
//
//	go run ./testapp -otlp-endpoint http://localhost:4318/auditlogs -filestore C:\temp\auditlog-demo
//
// Override the HMAC key via environment (see go.opentelemetry.io/otel/sdk/auditlog):
//
//	set OTEL_AUDITLOG_HMAC_KEY=your-secret && go run ./testapp
//
//	set OTEL_AUDITLOG_HMAC_KEY_FILE=C:\path\to\key.txt && go run ./testapp
//
// Or pass a key file path only for this run (when OTEL_* HMAC env vars are unset):
//
//	go run ./testapp -hmac-key-file C:\...\sdk\auditlog\testapp\dev_hmac_key.txt
//
// Integrity uses HMAC (dev_hmac_key.txt), SHA-256 hash, and certificate signature (dev_sign_cert.pem / dev_sign_key.pem).
// Override signing PEMs with -sign-cert-file and -sign-key-file. Regenerate dev certs: go run ./testapp/examples/certgen
//
// Delivery is synchronous (direct export on emit, WaitOnExport). For async store-and-retry
// with -filestore durability, change SetDeliveryMode to AuditDeliveryModeAsyncStoreRetry in main.
//
// Each emitted audit record gets a unique audit.record.id (UUID) so new runs do not collide
// with rows still in a file-backed store from a previous run.
//
// Emit many records with a counter in each body and pause between emits:
//
//	go run ./testapp -count 100 -interval 10ms
//
// Use one event name for every record:
//
//	go run ./testapp -count 5 -event app.order.placed
//
// Reject every 3rd emit (invalid integrity proofs, status 400):
//
//	go run ./testapp -count 10 -reject-every 3
//
// Enable startup replay debug logs:
//
//	go run ./testapp -debug-replay -otlp-endpoint http://localhost:4318/auditlogs -filestore C:\temp\auditlog-demo
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unsafe"

	"github.com/google/uuid"

	"go.opentelemetry.io/otel/sdk/auditlog/otlpexport"
	"go.opentelemetry.io/otel/log"
	auditlog "go.opentelemetry.io/otel/sdk/auditlog"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

func main() {
	fileStoreDir := flag.String("filestore", "", "if set, persist pending records to this directory (file store); default is in-memory")
	count := flag.Int("count", 3, "number of audit records to emit (>= 1)")
	interval := flag.Duration("interval", 0, "optional delay between emits (e.g. 50ms, 1s); 0 means back-to-back")
	eventFixed := flag.String("event", "", "if set, every record uses this event name; otherwise events rotate user.login, resource.read, policy.deny")
	quiet := flag.Bool("quiet", false, "if set, only print exported JSON lines (no per-emit status lines)")
	printSent := flag.Bool("print-sent", true, "print each record as JSON to stderr immediately before export")
	otlpEndpoint := flag.String("otlp-endpoint", "", "if non-empty, export logs with OTLP HTTP to this URL (e.g. http://localhost:4318 or http://localhost:4318/auditlogs); path omitted uses /auditlogs; empty writes JSON lines to stdout")
	debugReplay := flag.Bool("debug-replay", false, "if set, enable audit startup replay debug logs (sets OTEL_AUDITLOG_DEBUG_REPLAY=1)")
	hmacKeyFile := flag.String("hmac-key-file", "", "path to HMAC key file; when OTEL_AUDITLOG_HMAC_KEY* are unset, overrides auto-discovery of testapp/dev_hmac_key.txt")
	signCertFile := flag.String("sign-cert-file", "", "path to PEM certificate for audit.signature; default testapp/dev_sign_cert.pem")
	signKeyFile := flag.String("sign-key-file", "", "path to PEM private key for signing; default testapp/dev_sign_key.pem")
	rejectEvery := flag.Int("reject-every", 0, "if > 0, every Nth emit (1-based) uses invalid integrity proofs so the provider rejects it (status 400)")
	flag.Parse()

	if *count < 1 {
		fmt.Fprintln(os.Stderr, "count must be >= 1")
		os.Exit(2)
	}
	if *rejectEvery < 0 {
		fmt.Fprintln(os.Stderr, "reject-every must be >= 0")
		os.Exit(2)
	}
	if *rejectEvery > 0 {
		fmt.Fprintf(os.Stderr, "testapp: every %d record(s) will use invalid integrity proofs (expected status 400)\n", *rejectEvery)
	}
	if *debugReplay {
		_ = os.Setenv("OTEL_AUDITLOG_DEBUG_REPLAY", "1")
		fmt.Fprintln(os.Stderr, "testapp: enabled replay debug logs (OTEL_AUDITLOG_DEBUG_REPLAY=1)")
	}

	envHMACFile := strings.TrimSpace(os.Getenv(auditlog.EnvAuditlogHMACKeyFile))
	envHMAC := strings.TrimSpace(os.Getenv(auditlog.EnvAuditlogHMACKey))
	if envHMACFile == "" && envHMAC == "" {
		path := strings.TrimSpace(*hmacKeyFile)
		if path == "" {
			path = resolveDefaultDevHMACKeyPath()
		}
		if path == "" {
			fmt.Fprintf(os.Stderr, "testapp: no HMAC key; set %s, %s, or -hmac-key-file, or run from a directory where testapp/dev_hmac_key.txt can be found\n",
				auditlog.EnvAuditlogHMACKeyFile, auditlog.EnvAuditlogHMACKey)
			os.Exit(2)
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "testapp: HMAC key file path: %v\n", err)
			os.Exit(2)
		}
		if err := os.Setenv(auditlog.EnvAuditlogHMACKeyFile, absPath); err != nil {
			fmt.Fprintf(os.Stderr, "testapp: set %s: %v\n", auditlog.EnvAuditlogHMACKeyFile, err)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "testapp: HMAC key from %s (override with %s or %s)\n",
			absPath, auditlog.EnvAuditlogHMACKeyFile, auditlog.EnvAuditlogHMACKey)
	}

	certPath, keyPath, err := resolveSignCertKeyPaths(*signCertFile, *signKeyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testapp: %v\n", err)
		os.Exit(2)
	}
	signatureSigner, err := auditlog.NewAuditCertificateSignatureSignerFromFiles(certPath, keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testapp: signature signer: %v\n", err)
		os.Exit(2)
	}
	signatureVerifier, err := auditlog.NewAuditCertificateSignatureVerifierFromFiles(certPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testapp: signature verifier: %v\n", err)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "testapp: signature cert %s key %s\n", certPath, keyPath)

	exporter, err := buildExporter(*otlpEndpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "exporter: %v\n", err)
		os.Exit(1)
	}
	if *printSent {
		if _, ok := exporter.(*stdoutExporter); !ok {
			exporter = &loggingExporter{inner: exporter}
		}
	}
	var store auditlog.AuditLogStore
	if *fileStoreDir != "" {
		store, err = auditlog.NewAuditLogFileStore(*fileStoreDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "file store: %v\n", err)
			os.Exit(1)
		}
		pending, perr := store.GetAll(context.Background())
		if perr == nil && len(pending) > 0 {
			fmt.Fprintf(os.Stderr, "testapp: file store contains %d pending record(s) from a prior async run; sync_direct does not replay them (switch delivery mode to async_store_retry to drain)\n", len(pending))
		}
		if fi, serr := os.Stat(*fileStoreDir); serr == nil && !fi.IsDir() {
			fmt.Fprintf(os.Stderr, "testapp: if this file still shows old lines after \"delivered\", close it in your editor (Windows locks) or use -filestore <directory> (data goes to <directory>\\audit.log)\n")
		}
	} else {
		store = auditlog.NewAuditLogInMemoryStore()
		if strings.TrimSpace(*otlpEndpoint) != "" {
			fmt.Fprintf(os.Stderr, "testapp: sync_direct delivery; records are not persisted to a store. Use async_store_retry with -filestore for durability across failed exports and restarts.\n")
		}
	}

	builder, err := auditlog.NewAuditLogProcessorBuilder(exporter, store)
	if err != nil {
		fmt.Fprintf(os.Stderr, "processor builder: %v\n", err)
		os.Exit(1)
	}
	processor, err := builder.
		SetDeliveryMode(auditlog.AuditDeliveryModeSyncDirect).
		SetWaitOnExport(true).
		SetExporterTimeout(10 * time.Second).
		Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "processor: %v\n", err)
		os.Exit(1)
	}

	integrityFields := auditlog.AuditIntegrityHMAC | auditlog.AuditIntegrityHash | auditlog.AuditIntegritySignature
	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(processor),
		auditlog.WithAuditHashAlgorithm("sha256"),
		auditlog.WithAuditHMACVerificationKeyFromEnvironment(),
		auditlog.WithAuditRecordSigning(integrityFields, auditlog.AuditSignContentBody),
		auditlog.WithAuditSignatureSigner(signatureSigner),
		auditlog.WithAuditSignatureVerifier(signatureVerifier),
	)

	logger := provider.Logger("testapp", auditlog.WithAuditLoggerVersion("0.0.1"))
	ctx := context.Background()

	templates := []struct {
		name   string
		action string
		ip     string
	}{
		{"user.login", "login", "192.0.2.10"},
		{"resource.read", "read", "192.0.2.11"},
		{"policy.deny", "access", "192.0.2.12"},
	}

	for i := 0; i < *count; i++ {
		var name, action, ip string
		if strings.TrimSpace(*eventFixed) != "" {
			name = strings.TrimSpace(*eventFixed)
			action = "emit"
			ip = fmt.Sprintf("192.0.2.%d", (i%254)+1)
		} else {
			t := templates[i%len(templates)]
			name = t.name
			action = t.action
			ip = t.ip
		}

		rec := buildAuditRecord(name, action, ip, i)
		if *rejectEvery > 0 && (i+1)%*rejectEvery == 0 {
			rec = malformedAuditRecord(rec)
			if !*quiet {
				fmt.Fprintf(os.Stderr, "testapp: malformed integrity injected for record_id=%s (emit %d, every %d)\n", rec.RecordID, i+1, *rejectEvery)
			}
		}
		res := logger.EmitWithResult(ctx, rec)
		if !*quiet {
			fmt.Printf("emit result: status=%d %s record_id=%s reason=%q\n", res.StatusCode, res.Status, rec.RecordID, res.Reason)
		}
		if *interval > 0 && i+1 < *count {
			time.Sleep(*interval)
		}
	}

	if err := provider.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "provider shutdown: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("done.")
}

func resolveSignCertKeyPaths(certFlag, keyFlag string) (string, string, error) {
	certPath := strings.TrimSpace(certFlag)
	keyPath := strings.TrimSpace(keyFlag)
	if certPath == "" {
		certPath = resolveDefaultDevFile("dev_sign_cert.pem")
	}
	if keyPath == "" {
		keyPath = resolveDefaultDevFile("dev_sign_key.pem")
	}
	if certPath == "" || keyPath == "" {
		return "", "", fmt.Errorf("no signing cert/key; use -sign-cert-file and -sign-key-file, or run from a directory where testapp/dev_sign_*.pem can be found (go run ./testapp/examples/certgen to generate)")
	}
	certAbs, err := filepath.Abs(certPath)
	if err != nil {
		return "", "", fmt.Errorf("sign cert path: %w", err)
	}
	keyAbs, err := filepath.Abs(keyPath)
	if err != nil {
		return "", "", fmt.Errorf("sign key path: %w", err)
	}
	return certAbs, keyAbs, nil
}

func resolveDefaultDevFile(name string) string {
	try := func(p string) string {
		p = filepath.Clean(p)
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			return ""
		}
		return p
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if p := try(filepath.Join(exeDir, name)); p != "" {
			return p
		}
		if p := try(filepath.Join(exeDir, "testapp", name)); p != "" {
			return p
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	relCandidates := []string{
		filepath.Join("testapp", name),
		name,
		filepath.Join("sdk", "auditlog", "testapp", name),
	}
	for _, rel := range relCandidates {
		if p := try(filepath.Join(wd, rel)); p != "" {
			return p
		}
	}
	return ""
}

func resolveDefaultDevHMACKeyPath() string {
	return resolveDefaultDevFile("dev_hmac_key.txt")
}

func buildExporter(otlpURL string) (auditlog.Exporter, error) {
	raw := strings.TrimSpace(otlpURL)
	if raw == "" {
		return newStdoutExporter(), nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" {
		u, err = url.Parse("http://" + raw)
		if err != nil {
			return nil, err
		}
	}
	if u.Host == "" {
		return nil, fmt.Errorf("otlp-endpoint: missing host in %q", raw)
	}
	opts := []otlpexport.Option{otlpexport.WithEndpoint(u.Host)}
	switch u.Scheme {
	case "http":
		opts = append(opts, otlpexport.WithInsecure())
	case "https":
	default:
		return nil, fmt.Errorf("otlp-endpoint: unsupported scheme %q (use http or https)", u.Scheme)
	}
	if p := strings.TrimSuffix(u.EscapedPath(), "/"); p != "" {
		opts = append(opts, otlpexport.WithURLPath(p))
	}
	return otlpexport.NewHTTP(context.Background(), opts...)
}

type loggingExporter struct {
	inner auditlog.Exporter
}

func (e *loggingExporter) Export(ctx context.Context, records []auditlog.Record) (auditlog.ExportResult, error) {
	for _, r := range records {
		if err := printRecordJSON(os.Stderr, "sending", &r); err != nil {
			return auditlog.ExportResult{}, err
		}
	}
	return e.inner.Export(ctx, records)
}

func (e *loggingExporter) Shutdown(ctx context.Context) error   { return e.inner.Shutdown(ctx) }
func (e *loggingExporter) ForceFlush(ctx context.Context) error { return e.inner.ForceFlush(ctx) }

type stdoutExporter struct{}

func newStdoutExporter() auditlog.Exporter {
	return &stdoutExporter{}
}

func (e *stdoutExporter) Export(ctx context.Context, records []auditlog.Record) (auditlog.ExportResult, error) {
	for _, r := range records {
		if err := printRecordJSON(os.Stdout, "", &r); err != nil {
			return auditlog.ExportResult{}, err
		}
	}
	return auditlog.ExportOK(records), nil
}

func (e *stdoutExporter) Shutdown(context.Context) error   { return nil }
func (e *stdoutExporter) ForceFlush(context.Context) error { return nil }

func printRecordJSON(w *os.File, label string, r *auditlog.Record) error {
	b, err := recordToJSON(r)
	if err != nil {
		return err
	}
	if label != "" {
		fmt.Fprintf(w, "%s:\n", label)
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func recordToJSON(r *auditlog.Record) ([]byte, error) {
	m := map[string]any{
		"timestamp": r.Timestamp().UTC().Format(time.RFC3339Nano),
		"severity":  r.Severity().String(),
	}
	bodyStr := r.Body().String()
	var bodyParsed any
	if err := json.Unmarshal([]byte(bodyStr), &bodyParsed); err == nil {
		m["body"] = bodyParsed
	} else {
		m["body"] = bodyStr
	}
	r.WalkAttributes(func(kv log.KeyValue) bool {
		m[kv.Key] = formatLogValue(kv.Value)
		return true
	})
	return json.MarshalIndent(m, "", "  ")
}

func formatLogValue(v log.Value) string {
	switch v.Kind() {
	case log.KindString:
		return v.AsString()
	case log.KindEmpty:
		return ""
	default:
		return v.String()
	}
}

func malformedAuditRecord(rec auditlog.AuditRecord) auditlog.AuditRecord {
	rec.HMAC = "0000000000000000000000000000000000000000000000000000000000000000"
	rec.Hash = "0000000000000000000000000000000000000000000000000000000000000000"
	rec.Signature = "invalid-audit-signature"
	return rec
}

func buildAuditRecord(eventName, action, sourceIP string, iter int) auditlog.AuditRecord {
	now := time.Now().UTC()
	recordID := "rec-" + uuid.NewString()
	base := newAuditBaseRecord()
	base.SetTimestamp(now)
	base.SetObservedTimestamp(now)
	base.SetSeverity(log.SeverityInfo)
	body := fmt.Sprintf(`{"event":%q,"n":%d,"id":%q}`, eventName, iter, recordID)
	base.SetBody(log.StringValue(body))
	base.AddAttributes(
		log.String("audit.record.id", recordID),
		log.String("base", "testapp"),
	)

	return auditlog.AuditRecord{
		Record:        base,
		EventName:     eventName,
		Actor:         log.StringValue("alice@example.com"),
		ActorType:     "user",
		Action:        action,
		Resource:      log.StringValue("/api/widgets"),
		Outcome:       "success",
		SourceIP:      sourceIP,
		RecordID:      recordID,
		SchemaVersion: "1.0",
		SignContent:   "body",
	}
}

func newAuditBaseRecord() auditlog.Record {
	r := new(sdklog.Record)
	setSDKRecordField(r, "attributeValueLengthLimit", -1)
	setSDKRecordField(r, "attributeCountLimit", -1)
	return *r
}

func setSDKRecordField(r *sdklog.Record, name string, value any) {
	rVal := reflect.ValueOf(r).Elem()
	rf := rVal.FieldByName(name)
	rf = reflect.NewAt(rf.Type(), unsafe.Pointer(rf.UnsafeAddr())).Elem()
	rf.Set(reflect.ValueOf(value))
}
