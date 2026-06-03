// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import "context"

// ExportOK returns a successful export result with locally computed receipts.
func ExportOK(records []Record) ExportResult {
	return ExportResult{Receipts: ReceiptsFromRecords(records)}
}

// ExportOKErr is a helper for mock exporters.
func ExportOKErr(ctx context.Context, records []Record) (ExportResult, error) {
	_ = ctx
	return ExportOK(records), nil
}
