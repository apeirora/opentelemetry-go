# E2E manual test runner for auditlog SDK + collector + flaky sink
$ErrorActionPreference = "Continue"

$Root = Split-Path -Parent $PSScriptRoot
$TestLogs = $PSScriptRoot
$CollectorBin = "C:\Users\m.jarmolkiewicz\OTEL\opentelemetry-collector-contrib\bin\otelauditcol_windows_amd64.exe"
$CollectorCfg = "C:\Users\m.jarmolkiewicz\OTEL\opentelemetry-collector-contrib\receiver\auditlogreceiver\example-config.yaml"
$SinkExe = "C:\Users\m.jarmolkiewicz\OTEL\opentelemetry-collector-contrib\test-standalone\flaky-otlp-backend.exe"
$SinkDir = "C:\Users\m.jarmolkiewicz\OTEL\opentelemetry-collector-contrib\test-standalone"
$TestAppDir = Join-Path $Root "sdk\auditlog"
$OtlpEndpoint = "https://localhost:4310/v1/audit"

function Stop-E2EServices {
    foreach ($port in @(4310, 9999)) {
        $lines = netstat -ano | Select-String ":$port\s"
        foreach ($line in $lines) {
            if ($line -match '\s+(\d+)\s*$') {
                $procId = [int]$matches[1]
                if ($procId -gt 0) {
                    taskkill /PID $procId /F 2>$null | Out-Null
                }
            }
        }
    }
    Get-Process -Name "otelauditcol_windows_amd64","flaky-otlp-backend" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2
}

function Start-Sink {
    param([string[]]$SinkCliArgs, [string]$LogFile)
    $quoted = ($SinkCliArgs | ForEach-Object { if ($_ -match '\s') { "`"$_`"" } else { $_ } }) -join ' '
    $cmd = "& '$SinkExe' $quoted *> '$LogFile'"
    return Start-Process powershell -ArgumentList "-NoProfile", "-Command", $cmd -PassThru -WindowStyle Hidden
}

function Start-Collector {
    param([string]$LogFile)
    $contribRoot = "C:\Users\m.jarmolkiewicz\OTEL\opentelemetry-collector-contrib"
    $cmd = "Set-Location '$contribRoot'; & '$CollectorBin' --config='$CollectorCfg' *> '$LogFile'"
    return Start-Process powershell -ArgumentList "-NoProfile", "-Command", $cmd -PassThru -WindowStyle Hidden
}

function Wait-Port {
    param([int]$Port, [int]$TimeoutSec = 45)
    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    while ((Get-Date) -lt $deadline) {
        $listening = netstat -ano | Select-String ":$Port\s+.*LISTENING"
        if ($listening) { return $true }
        Start-Sleep -Milliseconds 500
    }
    return $false
}

function Run-TestApp {
    param([string[]]$TestAppCliArgs, [string]$LogFile)
    Push-Location $TestAppDir
    try {
        $allArgs = @("run", "./testapp", "-otlp-endpoint", $OtlpEndpoint) + $TestAppCliArgs
        $output = & go @allArgs 2>&1
        $exitCode = $LASTEXITCODE
        $output | Out-File -FilePath $LogFile -Encoding utf8
        return $exitCode
    } finally {
        Pop-Location
    }
}

$scenarios = @(
    @{
        Name = "01-happy-path"
        Readme = @"
# 01 Happy Path

## Setup
- Sink: accept all (`-reject-every 0`)
- SDK: 5 valid records, sync export when collector is up, mTLS to collector :4310

## Expected behavior
- **SDK**: each emit returns `status=200 delivered`
- **Collector** (`auditlogreceiver` sync): verifies HMAC via `certificatelogverify`, forwards to debug + OTLP HTTP sink
- **Sink**: 5 log records accepted (1 per HTTP request in sync mode)

## Observed
(Filled in after run — see testapp.log, collector.log, sink.log)
"@
        SinkCliArgs = @("-reject-every", "0")
        TestAppCliArgs = @("-count", "5", "-quiet")
        WaitAfterStart = 6
    },
    @{
        Name = "02-sdk-integrity-reject"
        Readme = @"
# 02 SDK Integrity Reject

## Setup
- Sink: accept all
- SDK: 9 records, `-reject-every 3` (every 3rd record has invalid HMAC)

## Expected behavior
- **SDK**: records 3,6,9 return `status=400` with integrity rejection reason
- **Collector**: `certificatelogverify` rejects bad records (permanent); valid records still exported
- **Sink**: receives only the 6 valid records

## Observed
(Filled in after run)
"@
        SinkCliArgs = @("-reject-every", "0")
        TestAppCliArgs = @("-count", "9", "-reject-every", "3")
        WaitAfterStart = 6
    },
    @{
        Name = "03-backend-503-intermittent"
        Readme = @"
# 03 Backend 503 Intermittent

## Setup
- Sink: reject every 3rd OTLP export with HTTP 503
- SDK: 9 valid records, 100ms interval

## Expected behavior
- **Collector**: sync mode WAL in Redis; retries transient 503 from backend exporter
- **SDK**: may see 503 on some emits until collector retries succeed; eventual delivery expected
- **Sink**: `received=9 accepted=9` after retries (extra HTTP requests from retries)

## Observed
(Filled in after run)
"@
        SinkCliArgs = @("-reject-every", "3", "-reject-code", "503")
        TestAppCliArgs = @("-count", "9", "-interval", "100ms")
        WaitAfterStart = 8
    },
    @{
        Name = "04-stress-throughput"
        Readme = @"
# 04 Stress Throughput

## Setup
- Sink: accept all
- SDK: 30 records, 10ms interval

## Expected behavior
- All 30 records delivered without integrity errors
- Collector circuit breaker stays closed under moderate load

## Observed
(Filled in after run)
"@
        SinkCliArgs = @("-reject-every", "0")
        TestAppCliArgs = @("-count", "30", "-interval", "10ms", "-quiet")
        WaitAfterStart = 6
    },
    @{
        Name = "05-backend-always-reject"
        Readme = @"
# 05 Backend Always Reject

## Setup
- Sink: reject every request (`-reject-every 1`) with 503
- SDK: 3 valid records, 500ms interval

## Expected behavior
- **Collector**: cannot complete export to backend; returns 503 to SDK
- **SDK**: `status=503` (or export failure) — HTTP errors from a reachable collector are not stored locally
- **Sink**: `accepted=0`, all requests REJECTED

## Observed
(Filled in after run)
"@
        SinkCliArgs = @("-reject-every", "1", "-reject-code", "503")
        TestAppCliArgs = @("-count", "3", "-interval", "500ms")
        WaitAfterStart = 8
    },
    @{
        Name = "06-backend-429-rate-limit"
        Readme = @"
# 06 Backend 429 Rate Limit

## Setup
- Sink: reject every 2nd request with HTTP 429
- SDK: 8 valid records, 150ms interval

## Expected behavior
- Collector treats 429 as retryable; eventual delivery of all 8 records
- Sink shows alternating REJECTED 429 / ACCEPTED 200

## Observed
(Filled in after run)
"@
        SinkCliArgs = @("-reject-every", "2", "-reject-code", "429")
        TestAppCliArgs = @("-count", "8", "-interval", "150ms")
        WaitAfterStart = 10
    },
    @{
        Name = "07-partial-sdk-and-backend"
        Readme = @"
# 07 Partial SDK + Backend Failures

## Setup
- Sink: reject every 4th request with 503
- SDK: 12 records, every 4th invalid integrity, 100ms interval

## Expected behavior
- SDK integrity failures (records 4,8,12): `status=400`
- Remaining 9 valid records face intermittent backend 503; collector retries

## Observed
(Filled in after run)
"@
        SinkCliArgs = @("-reject-every", "4", "-reject-code", "503")
        TestAppCliArgs = @("-count", "12", "-reject-every", "4", "-interval", "100ms")
        WaitAfterStart = 12
    }
)

Write-Host "=== Building flaky sink ==="
Push-Location $SinkDir
go build -o flaky-otlp-backend.exe . 2>&1
if ($LASTEXITCODE -ne 0) { throw "sink build failed" }
Pop-Location

Stop-E2EServices

foreach ($scenario in $scenarios) {
    $dir = Join-Path $TestLogs $scenario.Name
    New-Item -ItemType Directory -Force -Path $dir | Out-Null

    Write-Host "`n========== Running $($scenario.Name) =========="
    Stop-E2EServices

    $sinkLog = Join-Path $dir "sink.log"
    $collectorLog = Join-Path $dir "collector.log"
    $testappLog = Join-Path $dir "testapp.log"

    Remove-Item $sinkLog, $collectorLog, $testappLog -ErrorAction SilentlyContinue

    $sinkProc = Start-Sink -SinkCliArgs $scenario.SinkCliArgs -LogFile $sinkLog
    if (-not (Wait-Port -Port 9999)) {
        Write-Host "ERROR: sink port 9999 not ready" -ForegroundColor Red
    }

    $collectorProc = Start-Collector -LogFile $collectorLog
    if (-not (Wait-Port -Port 4310)) {
        Write-Host "ERROR: collector port 4310 not ready" -ForegroundColor Red
    }

    Start-Sleep -Seconds $scenario.WaitAfterStart

    $meta = @"
scenario=$($scenario.Name)
started=$(Get-Date -Format o)
sink_args=$($scenario.SinkCliArgs -join ' ')
testapp_args=$($scenario.TestAppCliArgs -join ' ')
otlp_endpoint=$OtlpEndpoint
"@
    Set-Content -Path (Join-Path $dir "meta.txt") -Value $meta -Encoding UTF8

    $exitCode = Run-TestApp -TestAppCliArgs $scenario.TestAppCliArgs -LogFile $testappLog
    Add-Content -Path (Join-Path $dir "meta.txt") -Value "testapp_exit=$exitCode"

    Start-Sleep -Seconds 2
    Stop-E2EServices

    Write-Host "Completed $($scenario.Name) (exit=$exitCode)"
}

Write-Host "`n=== All scenarios finished. Logs in $TestLogs ==="
