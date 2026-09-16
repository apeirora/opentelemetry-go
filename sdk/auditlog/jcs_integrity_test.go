// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
)

func TestJCSCanonicalMatchesSigningProcessorShape(t *testing.T) {
	now := time.Unix(0, 1779096939611093600).UTC()
	base := Record{}
	base.SetTimestamp(now)
	base.SetObservedTimestamp(now)
	base.SetBody(log.StringValue(`{"event":"user.login"}`))
	base.AddAttributes(
		log.Int64("custom.count", 7),
		log.Bool("custom.ok", true),
		log.Float64("custom.ratio", 0.25),
		log.String(auditAttrIntegrityValue, "ignored"),
		log.String("sign_content", "meta"),
	)
	base.SetSeverity(log.SeverityInfo)
	base.SetSeverityText("INFO")

	rec := AuditRecord{
		Record:        base,
		EventName:     "user.login",
		Actor:         log.StringValue("alice@example.com"),
		ActorType:     "user",
		Action:        "login",
		Outcome:       "success",
		RecordID:      "rec-typed",
		SchemaVersion: "1.0",
	}

	canonical, err := jcsSigningPayload(rec)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(canonical)
	if !strings.Contains(payload, `"event_name":"user.login"`) {
		t.Fatalf("missing event_name: %s", payload)
	}
	if !strings.Contains(payload, `"body":{"stringValue":"{\"event\":\"user.login\"}"}`) {
		t.Fatalf("missing typed body: %s", payload)
	}
	if !strings.Contains(payload, `"timestamp":"1779096939611093600"`) {
		t.Fatalf("missing quoted timestamp: %s", payload)
	}
	if !strings.Contains(payload, `"observed_timestamp":"1779096939611093600"`) {
		t.Fatalf("missing quoted observed_timestamp: %s", payload)
	}
	if !strings.Contains(payload, `"custom.count":{"intValue":"7"}`) {
		t.Fatalf("missing typed int attr: %s", payload)
	}
	if !strings.Contains(payload, `"custom.ok":{"boolValue":true}`) {
		t.Fatalf("missing typed bool attr: %s", payload)
	}
	if !strings.Contains(payload, `"custom.ratio":{"doubleValue":0.25}`) {
		t.Fatalf("missing typed double attr: %s", payload)
	}
	if !strings.Contains(payload, `"audit.actor.id":{"stringValue":"alice@example.com"}`) {
		t.Fatalf("missing merged audit.actor.id: %s", payload)
	}
	for _, banned := range []string{`"severity_number"`, `"severity_text"`, auditAttrIntegrityValue, `"sign_content"`, `"attributes":[`} {
		if strings.Contains(payload, banned) {
			t.Fatalf("payload must not contain %q: %s", banned, payload)
		}
	}
}

func TestJCSCanonicalMatchesSigningPRCanonicalFixture(t *testing.T) {
	now := time.Unix(0, 1779096939611093600).UTC()
	base := Record{}
	base.SetTimestamp(now)
	base.SetObservedTimestamp(now)
	base.SetBody(log.StringValue(`{"event":"user.login"}`))

	rec := AuditRecord{
		Record:    base,
		EventName: "user.login",
		Actor:     log.StringValue("alice@example.com"),
		ActorType: "user",
		Action:    "login",
		Outcome:   "success",
		SourceIP:  "testapp",
		RecordID:  "rec-docker-pass",
	}

	canonical, err := jcsSigningPayload(rec)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"attributes":{"audit.action":{"stringValue":"login"},"audit.actor.id":{"stringValue":"alice@example.com"},"audit.actor.type":{"stringValue":"user"},"audit.outcome":{"stringValue":"success"},"audit.record.id":{"stringValue":"rec-docker-pass"},"audit.source.id":{"stringValue":"testapp"}},"body":{"stringValue":"{\"event\":\"user.login\"}"},"event_name":"user.login","observed_timestamp":"1779096939611093600","timestamp":"1779096939611093600"}`
	if got := string(canonical); got != want {
		t.Fatalf("canonical mismatch\ngot:  %s\nwant: %s", got, want)
	}
}

func TestTimestampPrecision(t *testing.T) {
	base := time.Unix(0, 1714041600000000000).UTC()

	r1 := AuditRecord{Record: Record{}, EventName: "e"}
	r1.SetTimestamp(base)
	r2 := AuditRecord{Record: Record{}, EventName: "e"}
	r2.SetTimestamp(base.Add(time.Nanosecond))

	b1, err := jcsSigningPayload(r1)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := jcsSigningPayload(r2)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(b1, b2) {
		t.Fatalf("timestamps differing by 1ns produced identical canonical bytes: %s", b1)
	}
}

func TestInt64AttributePrecision(t *testing.T) {
	const base int64 = 9_007_199_254_740_992

	r1 := AuditRecord{Record: Record{}, EventName: "e"}
	r1.AddAttributes(log.Int64("snowflake.id", base))
	r2 := AuditRecord{Record: Record{}, EventName: "e"}
	r2.AddAttributes(log.Int64("snowflake.id", base+1))

	b1, err := jcsSigningPayload(r1)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := jcsSigningPayload(r2)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(b1, b2) {
		t.Fatalf("int64 attrs differing by 1 beyond 2^53 produced identical canonical bytes: %s", b1)
	}
}

func TestMaxInt64SerializedAsString(t *testing.T) {
	rec := AuditRecord{Record: Record{}, EventName: "e"}
	rec.AddAttributes(log.Int64("big.value", math.MaxInt64))
	b, err := jcsSigningPayload(rec)
	if err != nil {
		t.Fatal(err)
	}
	want := `"intValue":"9223372036854775807"`
	if !strings.Contains(string(b), want) {
		t.Fatalf("canonical output does not contain quoted MaxInt64 %s; got: %s", want, b)
	}
}

func TestScalarTypeCollision(t *testing.T) {
	cases := []struct {
		name string
		setA func(*AuditRecord)
		setB func(*AuditRecord)
	}{
		{
			name: "int vs string with same decimal representation",
			setA: func(r *AuditRecord) { r.AddAttributes(log.Int64("k", 123)) },
			setB: func(r *AuditRecord) { r.AddAttributes(log.String("k", "123")) },
		},
		{
			name: "bytes vs string with same base64 representation",
			setA: func(r *AuditRecord) { r.AddAttributes(log.Bytes("k", []byte("hello"))) },
			setB: func(r *AuditRecord) { r.AddAttributes(log.String("k", "aGVsbG8=")) },
		},
		{
			name: "bool true vs string true",
			setA: func(r *AuditRecord) { r.AddAttributes(log.Bool("k", true)) },
			setB: func(r *AuditRecord) { r.AddAttributes(log.String("k", "true")) },
		},
		{
			name: "double 1.0 vs string 1",
			setA: func(r *AuditRecord) { r.AddAttributes(log.Float64("k", 1.0)) },
			setB: func(r *AuditRecord) { r.AddAttributes(log.String("k", "1")) },
		},
		{
			name: "negative int vs string with same decimal representation",
			setA: func(r *AuditRecord) { r.AddAttributes(log.Int64("k", -5)) },
			setB: func(r *AuditRecord) { r.AddAttributes(log.String("k", "-5")) },
		},
		{
			name: "slice of int vs slice of string with same value",
			setA: func(r *AuditRecord) {
				r.AddAttributes(log.Slice("k", log.Int64Value(7)))
			},
			setB: func(r *AuditRecord) {
				r.AddAttributes(log.Slice("k", log.StringValue("7")))
			},
		},
		{
			name: "slice of negative int vs slice of string with same decimal representation",
			setA: func(r *AuditRecord) {
				r.AddAttributes(log.Slice("k", log.Int64Value(-5)))
			},
			setB: func(r *AuditRecord) {
				r.AddAttributes(log.Slice("k", log.StringValue("-5")))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rA := AuditRecord{Record: Record{}, EventName: "e"}
			tc.setA(&rA)
			rB := AuditRecord{Record: Record{}, EventName: "e"}
			tc.setB(&rB)

			bA, err := jcsSigningPayload(rA)
			if err != nil {
				t.Fatalf("serialize A: %v", err)
			}
			bB, err := jcsSigningPayload(rB)
			if err != nil {
				t.Fatalf("serialize B: %v", err)
			}
			if bytes.Equal(bA, bB) {
				t.Fatalf("scalar type collision produced identical canonical bytes: %s", bA)
			}
		})
	}
}

func TestBodyTypedForEveryKind(t *testing.T) {
	cases := []struct {
		name string
		body log.Value
		want string
	}{
		{name: "string", body: log.StringValue("hello"), want: `"body":{"stringValue":"hello"}`},
		{name: "int", body: log.Int64Value(42), want: `"body":{"intValue":"42"}`},
		{name: "double", body: log.Float64Value(1.5), want: `"body":{"doubleValue":1.5}`},
		{name: "bool", body: log.BoolValue(true), want: `"body":{"boolValue":true}`},
		{name: "bytes", body: log.BytesValue([]byte{0xDE, 0xAD}), want: `"body":{"bytesValue":"3q0="}`},
		{name: "slice", body: log.SliceValue(log.StringValue("x")), want: `"body":[{"stringValue":"x"}]`},
		{name: "map", body: log.MapValue(log.String("action", "delete-all")), want: `"body":{"action":{"stringValue":"delete-all"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := AuditRecord{Record: Record{}, EventName: "e", SignContent: string(AuditSignContentBody)}
			rec.SetBody(tc.body)
			b, err := jcsSigningPayload(rec)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), tc.want) {
				t.Fatalf("missing %s in %s", tc.want, b)
			}
		})
	}
}

func TestTraceAndSpanIncluded(t *testing.T) {
	rec := AuditRecord{Record: Record{}, EventName: "e"}
	rec.SetTraceID(trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	rec.SetSpanID(trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8})
	b, err := jcsSigningPayload(rec)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(b)
	if !strings.Contains(payload, `"trace_id":"0102030405060708090a0b0c0d0e0f10"`) {
		t.Fatalf("missing trace_id: %s", payload)
	}
	if !strings.Contains(payload, `"span_id":"0102030405060708"`) {
		t.Fatalf("missing span_id: %s", payload)
	}
}

func TestRejectInvalidUTF8(t *testing.T) {
	rec := AuditRecord{Record: Record{}, EventName: "e"}
	rec.SetBody(log.StringValue(string([]byte{0xff, 0xfe, 0xfd})))
	_, err := jcsSigningPayload(rec)
	if err == nil {
		t.Fatal("expected invalid UTF-8 error")
	}
	if !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRejectDeepNesting(t *testing.T) {
	cur := log.String("leaf", "x")
	for i := 0; i < jsonMaxDepth+2; i++ {
		cur = log.Map("n", cur)
	}
	rec := AuditRecord{Record: Record{}, EventName: "e"}
	rec.AddAttributes(cur)
	_, err := jcsSigningPayload(rec)
	if err == nil {
		t.Fatal("expected nesting depth error")
	}
	if !strings.Contains(err.Error(), "nesting depth") {
		t.Fatalf("unexpected error: %v", err)
	}
}
