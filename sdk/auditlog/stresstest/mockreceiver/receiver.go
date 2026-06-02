// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mockreceiver

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	collogpb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	cpb "go.opentelemetry.io/proto/otlp/common/v1"
	lpb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"
)

const auditRecordIDAttr = "audit.record.id"

var emptyExportLogsServiceResponse = func() []byte {
	body := collogpb.ExportLogsServiceResponse{}
	r, err := proto.Marshal(&body)
	if err != nil {
		panic(err)
	}
	return r
}()

type FailBehavior int

const (
	FailBehaviorNone FailBehavior = iota
	FailBehaviorHTTP503
	FailBehaviorTimeout
)

type Config struct {
	URLPath string

	FailEveryN   int
	FailBehavior FailBehavior
	FailDelay    time.Duration

	StartAccepting bool
}

type Receiver struct {
	cfg Config

	listener net.Listener
	srv      *http.Server

	accepting atomic.Bool

	requestsTotal   atomic.Uint64
	failedRequests  atomic.Uint64
	acceptedRecords atomic.Uint64

	uniqueMu sync.Mutex
	unique   map[string]struct{}

	orderMu       sync.Mutex
	acceptedOrder []int
}

func Start(cfg Config) (*Receiver, error) {
	if cfg.URLPath == "" {
		cfg.URLPath = "/auditlogs"
	}
	if cfg.FailEveryN > 0 && cfg.FailBehavior == FailBehaviorNone {
		cfg.FailBehavior = FailBehaviorHTTP503
	}
	if cfg.FailBehavior == FailBehaviorTimeout && cfg.FailDelay <= 0 {
		cfg.FailDelay = 5 * time.Second
	}

	r := &Receiver{
		cfg:    cfg,
		unique: make(map[string]struct{}),
	}
	if cfg.StartAccepting {
		r.accepting.Store(true)
	} else {
		r.accepting.Store(false)
	}

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	r.listener = ln

	mux := http.NewServeMux()
	mux.Handle(cfg.URLPath, http.HandlerFunc(r.handleExport))
	r.srv = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	go func() { _ = r.srv.Serve(ln) }()
	return r, nil
}

func (r *Receiver) SetAccepting(v bool) {
	r.accepting.Store(v)
}

func (r *Receiver) Accepting() bool {
	return r.accepting.Load()
}

func (r *Receiver) HostPort() string {
	return r.listener.Addr().String()
}

func (r *Receiver) URLPath() string {
	return r.cfg.URLPath
}

func (r *Receiver) Close(ctx context.Context) error {
	if r.srv == nil {
		return nil
	}
	return r.srv.Shutdown(ctx)
}

func (r *Receiver) ResetStats() {
	r.requestsTotal.Store(0)
	r.failedRequests.Store(0)
	r.acceptedRecords.Store(0)
	r.uniqueMu.Lock()
	r.unique = make(map[string]struct{})
	r.uniqueMu.Unlock()
	r.orderMu.Lock()
	r.acceptedOrder = nil
	r.orderMu.Unlock()
}

func (r *Receiver) RequestsTotal() uint64 {
	return r.requestsTotal.Load()
}

func (r *Receiver) FailedRequests() uint64 {
	return r.failedRequests.Load()
}

func (r *Receiver) AcceptedRecords() uint64 {
	return r.acceptedRecords.Load()
}

func (r *Receiver) UniqueRecordCount() int {
	r.uniqueMu.Lock()
	defer r.uniqueMu.Unlock()
	return len(r.unique)
}

func (r *Receiver) HasRecordID(id string) bool {
	r.uniqueMu.Lock()
	defer r.uniqueMu.Unlock()
	_, ok := r.unique[id]
	return ok
}

func (r *Receiver) AcceptedSeqOrder() []int {
	r.orderMu.Lock()
	defer r.orderMu.Unlock()
	out := make([]int, len(r.acceptedOrder))
	copy(out, r.acceptedOrder)
	return out
}

func (r *Receiver) handleExport(w http.ResponseWriter, req *http.Request) {
	r.requestsTotal.Add(1)

	if !r.accepting.Load() {
		r.failedRequests.Add(1)
		http.Error(w, "receiver not accepting", http.StatusServiceUnavailable)
		return
	}

	n := r.requestsTotal.Load()
	if r.cfg.FailEveryN > 0 && n%uint64(r.cfg.FailEveryN) == 0 {
		r.failedRequests.Add(1)
		switch r.cfg.FailBehavior {
		case FailBehaviorTimeout:
			time.Sleep(r.cfg.FailDelay)
			http.Error(w, "simulated timeout", http.StatusGatewayTimeout)
		default:
			http.Error(w, "simulated unavailable", http.StatusServiceUnavailable)
		}
		return
	}

	body, err := readRequestBody(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ct := req.Header.Get("Content-Type"); ct != "application/x-protobuf" {
		http.Error(w, fmt.Sprintf("unsupported content-type: %s", ct), http.StatusUnsupportedMediaType)
		return
	}

	pbReq := &collogpb.ExportLogsServiceRequest{}
	if err := proto.Unmarshal(body, pbReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	records := countLogRecords(pbReq)
	ids := extractRecordIDs(pbReq)
	seqs := extractSeqs(pbReq)
	r.acceptedRecords.Add(uint64(records))
	r.trackUnique(ids)
	r.trackOrder(seqs)

	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(emptyExportLogsServiceResponse)
}

func (r *Receiver) trackUnique(ids []string) {
	if len(ids) == 0 {
		return
	}
	r.uniqueMu.Lock()
	defer r.uniqueMu.Unlock()
	for _, id := range ids {
		if id != "" {
			r.unique[id] = struct{}{}
		}
	}
}

func (r *Receiver) trackOrder(seqs []int) {
	if len(seqs) == 0 {
		return
	}
	r.orderMu.Lock()
	defer r.orderMu.Unlock()
	r.acceptedOrder = append(r.acceptedOrder, seqs...)
}

func readRequestBody(req *http.Request) ([]byte, error) {
	var reader io.ReadCloser
	switch req.Header.Get("Content-Encoding") {
	case "gzip":
		var err error
		reader, err = gzip.NewReader(req.Body)
		if err != nil {
			return nil, err
		}
	default:
		reader = req.Body
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func countLogRecords(req *collogpb.ExportLogsServiceRequest) int {
	n := 0
	for _, rl := range req.GetResourceLogs() {
		for _, sl := range rl.GetScopeLogs() {
			n += len(sl.GetLogRecords())
		}
	}
	return n
}

func extractRecordIDs(req *collogpb.ExportLogsServiceRequest) []string {
	var ids []string
	for _, rl := range req.GetResourceLogs() {
		for _, sl := range rl.GetScopeLogs() {
			for _, lr := range sl.GetLogRecords() {
				if id := recordIDFromLogRecord(lr); id != "" {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

func extractSeqs(req *collogpb.ExportLogsServiceRequest) []int {
	var seqs []int
	for _, rl := range req.GetResourceLogs() {
		for _, sl := range rl.GetScopeLogs() {
			for _, lr := range sl.GetLogRecords() {
				if seq, ok := seqFromLogRecord(lr); ok {
					seqs = append(seqs, seq)
				}
			}
		}
	}
	return seqs
}

func recordIDFromLogRecord(lr *lpb.LogRecord) string {
	for _, kv := range lr.GetAttributes() {
		if kv.GetKey() == auditRecordIDAttr {
			return stringValue(kv.GetValue())
		}
	}
	return ""
}

func seqFromLogRecord(lr *lpb.LogRecord) (int, bool) {
	body := stringValue(lr.GetBody())
	if body == "" {
		return 0, false
	}
	var payload struct {
		N int `json:"n"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return 0, false
	}
	return payload.N, true
}

func stringValue(v *cpb.AnyValue) string {
	if v == nil {
		return ""
	}
	if s, ok := v.Value.(*cpb.AnyValue_StringValue); ok {
		return s.StringValue
	}
	return ""
}

func WaitForUniqueRecords(ctx context.Context, r *Receiver, want int) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if r.UniqueRecordCount() >= want {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"timeout waiting for %d unique records (got %d accepted=%d requests=%d failed=%d): %w",
				want,
				r.UniqueRecordCount(),
				r.AcceptedRecords(),
				r.RequestsTotal(),
				r.FailedRequests(),
				ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func WaitForDrain(ctx context.Context, r *Receiver, want int, pending func() int) error {
	if err := WaitForUniqueRecords(ctx, r, want); err != nil {
		return err
	}
	for {
		if pending() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for store drain (pending=%d): %w", pending(), ctx.Err())
		}
	}
}

func WaitForStoreCount(ctx context.Context, pending func() int, want int) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if pending() == want {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for store count %d (got %d): %w", want, pending(), ctx.Err())
		case <-ticker.C:
		}
	}
}
