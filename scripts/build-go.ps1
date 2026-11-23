$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$previousEmbedded = $env:TW_EMBEDDED_BUILD
$previousRequired = $env:TW_REQUIRE_EMBEDDED_UI
$temporary = $null
Push-Location $root
try {
    $version = if ($env:TIMEWEAVER_BUILD_VERSION) { $env:TIMEWEAVER_BUILD_VERSION } else { 'dev' }
    $commit = $env:TIMEWEAVER_BUILD_COMMIT
    if (-not $commit) {
        $commit = & git rev-parse --short=12 HEAD 2>$null
        if ($LASTEXITCODE -ne 0) { $commit = 'archive' }
        else {
            $changes = & git status --porcelain --untracked-files=normal
            if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect the Git working tree.' }
            if ($changes) { $commit += '-dirty' }
        }
    }
    if ($version -notmatch '^[A-Za-z0-9._+-]+$' -or $commit -notmatch '^[A-Za-z0-9._+-]+$') {
        throw 'TIMEWEAVER_BUILD_VERSION and TIMEWEAVER_BUILD_COMMIT contain unsupported characters.'
    }
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
    $goOS = & go env GOOS
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine the Go build target.' }
    $suffix = if ($goOS -eq 'windows') { '.exe' } else { '' }
    $binary = "timeweaver$suffix"
    $temporary = Join-Path $root "dist/timeweaver-build-$([guid]::NewGuid().ToString('N'))$suffix"
    go build -trimpath -ldflags "-X main.version=$version -X main.commit=$commit" -o $temporary ./cmd/timeweaver
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
    & $temporary assets-check
    if ($LASTEXITCODE -ne 0) { throw 'Built executable embedded UI validation failed.' }
    Move-Item -LiteralPath $temporary -Destination (Join-Path $root "dist/$binary") -Force
    Write-Host "Built dist/$binary ($version, $commit), including the Vue UI."
    Write-Host "Run .\dist\$binary serve --env server/.env after database setup."
} finally {
    if ($temporary -and (Test-Path -LiteralPath $temporary)) { Remove-Item -LiteralPath $temporary -Force }
    $env:TW_EMBEDDED_BUILD = $previousEmbedded
    $env:TW_REQUIRE_EMBEDDED_UI = $previousRequired
    Pop-Location
}
