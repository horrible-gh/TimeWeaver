$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$previousEmbedded = $env:TW_EMBEDDED_BUILD
$previousRequired = $env:TW_REQUIRE_EMBEDDED_UI
Push-Location $root
try {
    npm ci --prefix client
    if ($LASTEXITCODE -ne 0) { throw 'Frontend dependency installation failed.' }
    $env:TW_EMBEDDED_BUILD = '1'
    npm run build --prefix client
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
    go run ./cmd/timeweaver assets-check
    if ($LASTEXITCODE -ne 0) { throw 'Embedded UI validation failed.' }
    $env:TW_REQUIRE_EMBEDDED_UI = '1'
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed.' }
    New-Item -ItemType Directory -Force dist | Out-Null
    $binary = if ($env:OS -eq 'Windows_NT') { 'timeweaver.exe' } else { 'timeweaver' }
    go build -trimpath -o (Join-Path 'dist' $binary) ./cmd/timeweaver
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
    Write-Host "Built dist/$binary (migration CLI; not a production API server yet)."
} finally {
    $env:TW_EMBEDDED_BUILD = $previousEmbedded
    $env:TW_REQUIRE_EMBEDDED_UI = $previousRequired
    Pop-Location
}
