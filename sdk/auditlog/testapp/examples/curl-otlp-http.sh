#!/usr/bin/env bash
# Sends one audit log record like testapp does with:
#   go run . -otlp-endpoint http://localhost:4318/auditlogs
#
# testapp uses OTLP HTTP/protobuf; this script uses OTLP/JSON (works when the
# collector OTLP HTTP receiver accepts application/json on the same path).

set -euo pipefail
ENDPOINT="${ENDPOINT:-http://localhost:4310/auditlogs}"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "POST ${ENDPOINT}"
echo "Body: ${DIR}/otlp-audit-login.json"
echo

curl -v -X POST "${ENDPOINT}" \
  -H "Content-Type: application/json" \
  -H "User-Agent: curl-otlp-auditlog-testapp-example" \
  --data-binary "@${DIR}/otlp-audit-login.json"
