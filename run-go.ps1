<#
.SYNOPSIS
Builds and runs the unified Go server in the foreground.
.DESCRIPTION
By default the Vue UI and Go executable are rebuilt on every invocation.
Use -NoBuild only to run an already validated dist/timeweaver.exe.
Use -Pull to update the current branch with git pull --ff-only first.
Existing configured databases are upgraded automatically by serve.
Use -Setup only for initial setup or explicit offline repair; it prompts if
the seed administrator needs a password. The script preserves server/.env.
#>
[CmdletBinding()]
param(
    [string]$EnvFile = 'server/.env',
    [switch]$Pull,
    [switch]$Setup,
    [switch]$NoBuild,
    [switch]$ForceBuild
)
$ErrorActionPreference = 'Stop'
$result = 1
Push-Location $PSScriptRoot
try {
    if ($NoBuild -and $ForceBuild) { throw '-NoBuild and -ForceBuild cannot be combined.' }
    if ($Pull) {
        Write-Host '[run] git pull --ff-only...'
        git pull --ff-only
        if ($LASTEXITCODE -ne 0) { throw 'Git update failed; server was not started.' }
    }
    if (-not $NoBuild) {
        Write-Host '[run] building the embedded UI and Go server...'
        & (Join-Path $PSScriptRoot 'scripts/build-go.ps1')
    }
    $name = if ($env:OS -eq 'Windows_NT') { 'timeweaver.exe' } else { 'timeweaver' }
    $binary = Join-Path $PSScriptRoot "dist/$name"
    if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) {
        throw "dist/$name is missing; run without -NoBuild."
    }
    & $binary version
    if ($LASTEXITCODE -ne 0) { throw 'Cannot read the server version.' }
    if ($Setup) {
        & $binary setup --env $EnvFile --allow-write
        if ($LASTEXITCODE -ne 0) { throw 'Database setup failed; server was not started.' }
    }
    # Serve owns migration and readiness; a read-only precheck would reject
    # routine pending upgrades before the server could apply them.
    Write-Host '[run] starting the server (automatic database upgrades and readiness checks)...'
    & $binary serve --env $EnvFile
    $result = $LASTEXITCODE
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
} finally {
    Pop-Location
}
exit $result
