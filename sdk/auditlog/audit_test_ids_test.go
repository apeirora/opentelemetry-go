// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package auditlog

import "fmt"

func testAuditRecordID(n int) string {
	return fmt.Sprintf("550e8400-e29b-41d4-a716-%012x", n)
}
