param(
    [string]$Python = "python",
    [switch]$SkipUiBuild
)

$ErrorActionPreference = "Stop"
$RootDir = Split-Path -Parent $PSScriptRoot

function Copy-IfMissing {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Target
    )

    if (-not (Test-Path -LiteralPath $Target)) {
        Copy-Item -LiteralPath $Source -Destination $Target
        Write-Host "Created $Target"
    } else {
        Write-Host "Kept existing $Target"
    }
}

function Setup-PythonProject {
    param([Parameter(Mandatory = $true)][string]$ProjectDir)

    Push-Location $ProjectDir
    try {
        & $Python -m venv .venv
        & ".\.venv\Scripts\python.exe" -m pip install --upgrade pip
        & ".\.venv\Scripts\pip.exe" install -r requirements.txt
    } finally {
        Pop-Location
    }
}

function Write-CommandFile {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Content
    )

    Set-Content -LiteralPath $Path -Value $Content -Encoding ASCII
    Write-Host "Created $Path"
}

New-Item -ItemType Directory -Force -Path `
    (Join-Path $RootDir "server\log"), `
    (Join-Path $RootDir "agent\log") | Out-Null

Setup-PythonProject (Join-Path $RootDir "server")
Copy-IfMissing `
    (Join-Path $RootDir "server\.env.sample") `
    (Join-Path $RootDir "server\.env")

Setup-PythonProject (Join-Path $RootDir "agent")
Copy-IfMissing `
    (Join-Path $RootDir "agent\conf\server.sample.json") `
    (Join-Path $RootDir "agent\conf\server.json")
Copy-IfMissing `
    (Join-Path $RootDir "agent\conf\time_weaver.sample.json") `
    (Join-Path $RootDir "agent\conf\time_weaver.json")

Push-Location (Join-Path $RootDir "client")
try {
    npm ci
    Copy-IfMissing `
        (Join-Path $RootDir "client\config.sample.js") `
        (Join-Path $RootDir "client\config.js")
    if (-not $SkipUiBuild) {
        npm run build
    }
} finally {
    Pop-Location
}

Write-CommandFile (Join-Path $RootDir "run-server.cmd") @"
@echo off
cd /d "%~dp0server"
".venv\Scripts\python.exe" -m uvicorn app:app --host 0.0.0.0 --port 8000 --workers 1
"@

Write-CommandFile (Join-Path $RootDir "run-agent.cmd") @"
@echo off
cd /d "%~dp0agent"
".venv\Scripts\python.exe" timeweaver.py
"@

Write-Host ""
Write-Host "TimeWeaver Windows setup complete."
Write-Host "Next:"
Write-Host "  1. Edit server\.env."
Write-Host "  2. Edit agent\conf\server.json and agent\conf\time_weaver.json."
Write-Host "  3. Run run-server.cmd and run-agent.cmd."
