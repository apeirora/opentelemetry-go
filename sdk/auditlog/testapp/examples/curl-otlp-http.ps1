# Sends one audit log record like testapp does with:
#   go run . -otlp-endpoint http://localhost:4318/auditlogs
#
# testapp uses OTLP HTTP/protobuf; this script uses OTLP/JSON (works when the
# collector OTLP HTTP receiver accepts application/json on the same path).
#
# Replace audit.hmac and timestamps with values from testapp -print-sent output
# if your collector validates them.

$Endpoint = "http://localhost:4310/auditlogs"
$PayloadFile = Join-Path $PSScriptRoot "otlp-audit-login.json"

Write-Host "POST $Endpoint"
Write-Host "Body: $PayloadFile"
Write-Host ""

curl.exe -v -X POST $Endpoint `
  -H "Content-Type: application/json" `
  -H "User-Agent: curl-otlp-auditlog-testapp-example" `
  --data-binary "@$PayloadFile"
