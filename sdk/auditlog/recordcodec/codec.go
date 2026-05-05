// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package recordcodec

import (
	"reflect"
	"strconv"
	"time"
	"unsafe"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"
)

type Data struct {
	Timestamp         time.Time `json:"timestamp"`
	ObservedTimestamp time.Time `json:"observed_timestamp"`
	EventName         string    `json:"event_name"`
	Severity          int32     `json:"severity"`
	SeverityText      string    `json:"severity_text"`
	Body              string    `json:"body"`
	BodyKind          int       `json:"body_kind"`
	Attributes        []KV      `json:"attributes"`
	TraceID           []byte    `json:"trace_id"`
	SpanID            []byte    `json:"span_id"`
	TraceFlags        uint8     `json:"trace_flags"`
}

type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Kind  int    `json:"kind"`
}

func Serialize(record *sdklog.Record) Data {
	data := Data{
		Timestamp:         record.Timestamp(),
		ObservedTimestamp: record.ObservedTimestamp(),
		EventName:         record.EventName(),
		Severity:          int32(record.Severity()),
		SeverityText:      record.SeverityText(),
		Body:              record.Body().String(),
		BodyKind:          int(record.Body().Kind()),
	}
	record.WalkAttributes(func(kv log.KeyValue) bool {
		value := kv.Value.String()
		switch kv.Value.Kind() {
		case log.KindString:
			value = kv.Value.AsString()
		case log.KindInt64:
			value = strconv.FormatInt(kv.Value.AsInt64(), 10)
		case log.KindFloat64:
			value = strconv.FormatFloat(kv.Value.AsFloat64(), 'f', -1, 64)
		case log.KindBool:
			value = strconv.FormatBool(kv.Value.AsBool())
		}
		data.Attributes = append(data.Attributes, KV{Key: kv.Key, Value: value, Kind: int(kv.Value.Kind())})
		return true
	})
	traceID := record.TraceID()
	spanID := record.SpanID()
	data.TraceID = traceID[:]
	data.SpanID = spanID[:]
	data.TraceFlags = uint8(record.TraceFlags())
	return data
}

func Deserialize(data Data) sdklog.Record {
	record := sdklog.Record{}
	setUnlimitedAttributeLimits(&record)
	record.SetTimestamp(data.Timestamp)
	record.SetObservedTimestamp(data.ObservedTimestamp)
	record.SetEventName(data.EventName)
	record.SetSeverity(log.Severity(data.Severity))
	record.SetSeverityText(data.SeverityText)
	switch log.Kind(data.BodyKind) {
	case log.KindInt64:
		if v, err := strconv.ParseInt(data.Body, 10, 64); err == nil {
			record.SetBody(log.Int64Value(v))
		} else {
			record.SetBody(log.StringValue(data.Body))
		}
	case log.KindFloat64:
		if v, err := strconv.ParseFloat(data.Body, 64); err == nil {
			record.SetBody(log.Float64Value(v))
		} else {
			record.SetBody(log.StringValue(data.Body))
		}
	case log.KindBool:
		if v, err := strconv.ParseBool(data.Body); err == nil {
			record.SetBody(log.BoolValue(v))
		} else {
			record.SetBody(log.StringValue(data.Body))
		}
	default:
		record.SetBody(log.StringValue(data.Body))
	}
	for _, attr := range data.Attributes {
		var value log.Value
		switch log.Kind(attr.Kind) {
		case log.KindString:
			value = log.StringValue(attr.Value)
		case log.KindInt64:
			if v, err := strconv.ParseInt(attr.Value, 10, 64); err == nil {
				value = log.Int64Value(v)
			} else {
				value = log.StringValue(attr.Value)
			}
		case log.KindFloat64:
			if v, err := strconv.ParseFloat(attr.Value, 64); err == nil {
				value = log.Float64Value(v)
			} else {
				value = log.StringValue(attr.Value)
			}
		case log.KindBool:
			if v, err := strconv.ParseBool(attr.Value); err == nil {
				value = log.BoolValue(v)
			} else {
				value = log.StringValue(attr.Value)
			}
		default:
			value = log.StringValue(attr.Value)
		}
		record.AddAttributes(log.KeyValue{Key: attr.Key, Value: value})
	}
	var traceID trace.TraceID
	var spanID trace.SpanID
	copy(traceID[:], data.TraceID)
	copy(spanID[:], data.SpanID)
	record.SetTraceID(traceID)
	record.SetSpanID(spanID)
	record.SetTraceFlags(trace.TraceFlags(data.TraceFlags))
	return record
}

func setUnlimitedAttributeLimits(r *sdklog.Record) {
	rVal := reflect.ValueOf(r).Elem()
	setLimit := func(name string, v int) {
		rf := rVal.FieldByName(name)
		rf = reflect.NewAt(rf.Type(), unsafe.Pointer(rf.UnsafeAddr())).Elem() //nolint:gosec // uintptr -> unsafe.Pointer; mirrors sdk/log/logtest.
		rf.Set(reflect.ValueOf(v))
	}
	setLimit("attributeCountLimit", -1)
	setLimit("attributeValueLengthLimit", -1)
}
