// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package recordcodec

import (
	"testing"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/auditlog/identity"
	"go.opentelemetry.io/otel/sdk/log/logtest"
)

func TestSerializeDeserializeGetRecordID(t *testing.T) {
	r := logtest.RecordFactory{
		Body: log.StringValue(`{"id":"R1"}`),
		Attributes: []log.KeyValue{
			log.String("audit.record.id", "rec-R1"),
			log.String("base", "v"),
		},
		AttributeValueLengthLimit: -1,
		AttributeCountLimit:       -1,
	}.NewRecord()
	data := Serialize(&r)
	r2 := Deserialize(data)
	id, err := identity.GetRecordID(&r2)
	if err != nil {
		t.Fatal(err)
	}
	if id != "rec-R1" {
		t.Fatalf("got %q want rec-R1", id)
	}
}
