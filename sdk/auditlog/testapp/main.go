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
// Or with a file-backed store (async durability demo):
//
//	go run ./testapp -filestore /tmp/auditlog-demo
//
// Each emitted audit record gets a unique audit.record_id (UUID) so new runs do not collide
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

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
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
	flag.Parse()

	if *count < 1 {
		fmt.Fprintln(os.Stderr, "count must be >= 1")
		os.Exit(2)
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
			fmt.Fprintf(os.Stderr, "testapp: file store contains %d pending record(s); processor will export them first\n", len(pending))
		}
		if fi, serr := os.Stat(*fileStoreDir); serr == nil && !fi.IsDir() {
			fmt.Fprintf(os.Stderr, "testapp: if this file still shows old lines after \"delivered\", close it in your editor (Windows locks) or use -filestore <directory> (data goes to <directory>\\audit.log)\n")
		}
	} else {
		store = auditlog.NewAuditLogInMemoryStore()
		if strings.TrimSpace(*otlpEndpoint) != "" {
			fmt.Fprintf(os.Stderr, "testapp: -filestore not set; pending audit records are only in memory (lost on exit). Use -filestore <dir> to persist across failed exports and restarts.\n")
		}
	}

	builder, err := auditlog.NewAuditLogProcessorBuilder(exporter, store)
	if err != nil {
		fmt.Fprintf(os.Stderr, "processor builder: %v\n", err)
		os.Exit(1)
	}
	processor, err := builder.
		SetWaitOnExport(true).
		SetScheduleDelay(200 * time.Millisecond).
		SetMaxExportBatchSize(32).
		SetExporterTimeout(10 * time.Second).
		Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "processor: %v\n", err)
		os.Exit(1)
	}

	provider := auditlog.NewAuditLoggerProvider(
		auditlog.WithAuditRecordProcessor(processor),
		auditlog.WithAuditHashAlgorithm("sha256"),
		auditlog.WithAuditHMACVerificationKeyFromEnvironment(),
		auditlog.WithAuditRecordSigning(auditlog.AuditIntegrityHMAC, auditlog.AuditSignContentBody),
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

func resolveDefaultDevHMACKeyPath() string {
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
		if p := try(filepath.Join(exeDir, "dev_hmac_key.txt")); p != "" {
			return p
		}
		if p := try(filepath.Join(exeDir, "testapp", "dev_hmac_key.txt")); p != "" {
			return p
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	relCandidates := []string{
		filepath.Join("testapp", "dev_hmac_key.txt"),
		"dev_hmac_key.txt",
		filepath.Join("sdk", "auditlog", "testapp", "dev_hmac_key.txt"),
	}
	for _, rel := range relCandidates {
		if p := try(filepath.Join(wd, rel)); p != "" {
			return p
		}
	}
	return ""
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
	opts := []otlploghttp.Option{otlploghttp.WithEndpoint(u.Host)}
	switch u.Scheme {
	case "http":
		opts = append(opts, otlploghttp.WithInsecure())
	case "https":
	default:
		return nil, fmt.Errorf("otlp-endpoint: unsupported scheme %q (use http or https)", u.Scheme)
	}
	if p := strings.TrimSuffix(u.EscapedPath(), "/"); p != "" {
		opts = append(opts, otlploghttp.WithURLPath(p))
	} else {
		opts = append(opts, otlploghttp.WithURLPath("/auditlogs"))
	}
	return otlploghttp.New(context.Background(), opts...)
}

type loggingExporter struct {
	inner auditlog.Exporter
}

func (e *loggingExporter) Export(ctx context.Context, records []auditlog.Record) error {
	for _, r := range records {
		if err := printRecordJSON(os.Stderr, "sending", &r); err != nil {
			return err
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

func (e *stdoutExporter) Export(ctx context.Context, records []auditlog.Record) error {
	for _, r := range records {
		if err := printRecordJSON(os.Stdout, "", &r); err != nil {
			return err
		}
	}
	return nil
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
		log.String("audit.record_id", recordID),
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
