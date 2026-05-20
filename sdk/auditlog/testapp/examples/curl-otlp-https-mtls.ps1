# HTTPS + client cert like testapp with bundled dev_otlp_client.crt/key:
#   go run . -otlp-endpoint https://localhost:4318/auditlogs

$Endpoint = "https://localhost:4310/auditlogs"
$PayloadFile = Join-Path $PSScriptRoot "otlp-audit-login.json"
$TestappDir = Split-Path $PSScriptRoot -Parent
$Cert = Join-Path $TestappDir "dev_otlp_client.crt"
$Key = Join-Path $TestappDir "dev_otlp_client.key"

curl.exe -v -X POST $Endpoint `
  -H "Content-Type: application/json" `
  -H "User-Agent: curl-otlp-auditlog-testapp-example" `
  --cert $Cert --key $Key `
  --data-binary "@$PayloadFile"
