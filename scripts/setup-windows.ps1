param(
    [string]$Python = "python",
    [ValidateSet("", "all", "server", "agent", "client")]
    [string]$Component = "",
    # Server config
    [ValidateSet("", "sqlite3", "mysql")]
    [string]$DbType = "",
    [string]$DbHost = "",
    [string]$DbPort = "",
    [string]$DbUser = "",
    [string]$DbPassword = "",
    [string]$DbName = "",
    [string]$DbSchema = "",
    [string]$DbPath = "",
    [string]$SecretKey = "",
    [string]$AllowedOrigin = "",
    [string]$Context = "",
    [string]$AccessTokenExpireMinutes = "",
    [string]$RedisHost = "",
    [string]$RedisPort = "",
    [string]$RedisDb = "",
    # Agent config
    [string]$DeviceName = "",
    [string]$RescheduleMinute = "",
    [string]$LogLevel = "",
    # Client config
    [string]$ApiUrl = "",
    # Behaviour
    [switch]$NonInteractive,
    [switch]$Reconfigure,
    [switch]$SkipUiBuild
)

$ErrorActionPreference = "Stop"
$RootDir = Split-Path -Parent $PSScriptRoot

# Auto-detect a non-interactive session so prompts never hang in CI / pipelines.
if (-not $NonInteractive) {
    if ($env:CI -or [Console]::IsInputRedirected) { $NonInteractive = $true }
}

function Ask {
    param([string]$Prompt, [string]$Default = "")
    if ($NonInteractive) { return $Default }
    if ($Default -ne "") {
        $ans = Read-Host "$Prompt [$Default]"
    } else {
        $ans = Read-Host "$Prompt"
    }
    if ([string]::IsNullOrWhiteSpace($ans)) { return $Default }
    return $ans.Trim()
}

function New-SecretKey {
    $bytes = New-Object byte[] 32
    [System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
    return -join ($bytes | ForEach-Object { $_.ToString('x2') })
}

# --- Component selection ------------------------------------------------------
if ($Component -eq "") {
    $Component = Ask "Install which component? (all/server/agent/client)" "all"
}
if ($Component -notin @("all", "server", "agent", "client")) {
    Write-Error "Invalid component: $Component (expected all|server|agent|client)"
    exit 2
}
$DoServer = $Component -in @("all", "server")
$DoAgent  = $Component -in @("all", "agent")
$DoClient = $Component -in @("all", "client")

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
    param([string]$Path, [string]$Content)
    Set-Content -LiteralPath $Path -Value $Content -Encoding ASCII
    Write-Host "Created $Path"
}

# --- Config generators (write ready-to-run config, no manual editing) ---------
function Write-ServerEnv {
    $target = Join-Path $RootDir "server\.env"
    if ((Test-Path -LiteralPath $target) -and -not $Reconfigure) {
        Write-Host "Using existing server\.env (pass -Reconfigure to regenerate)."
        return
    }

    $dbType = $DbType
    if ($dbType -eq "") { $dbType = Ask "Server database type (sqlite3/mysql)" "sqlite3" }

    $secret = $SecretKey
    if ($secret -eq "") { $secret = New-SecretKey }   # auto-generated, never "change-me"
    $origin = if ($AllowedOrigin -ne "") { $AllowedOrigin } else { "*" }
    $ctx    = if ($Context -ne "") { $Context } else { "/time_weaver" }
    $expire = if ($AccessTokenExpireMinutes -ne "") { $AccessTokenExpireMinutes } else { "30" }
    # Redis is optional at runtime (server falls back to an in-process token
    # blacklist). These values are only used when a Redis server is present.
    $rHost  = if ($RedisHost -ne "") { $RedisHost } else { "localhost" }
    $rPort  = if ($RedisPort -ne "") { $RedisPort } else { "6379" }
    $rDb    = if ($RedisDb   -ne "") { $RedisDb }   else { "0" }

    if ($dbType -eq "mysql") {
        $h  = if ($DbHost -ne "") { $DbHost } else { Ask "DB host" "127.0.0.1" }
        $p  = if ($DbPort -ne "") { $DbPort } else { Ask "DB port" "3306" }
        $u  = if ($DbUser -ne "") { $DbUser } else { Ask "DB user" "timeweaver" }
        $pw = if ($DbPassword -ne "") { $DbPassword } else { Ask "DB password" "" }
        $db = if ($DbName -ne "") { $DbName } else { Ask "DB name" "timeweaver" }
        $sc = if ($DbSchema -ne "") { $DbSchema } else { Ask "DB schema (blank if none)" "" }
        $dbPath = ""
    } else {
        $h = "127.0.0.1"; $p = "0"; $u = ""; $pw = ""; $db = ""; $sc = ""
        $dbPath = if ($DbPath -ne "") { $DbPath } else { "./timeweaver.sqlite3" }
    }

    $content = @"
ALLOWED_ORIGIN=$origin
SECRET_KEY=$secret
ACCESS_TOKEN_EXPIRE_MINUTES=$expire
CONTEXT=$ctx
DB_TYPE=$dbType
DB_HOST=$h
DB_PORT=$p
DB_USER=$u
DB_PASSWORD=$pw
DB_DATABASE=$db
DB_SCHEMA=$sc
DB_PATH=$dbPath
REDIS_HOST=$rHost
REDIS_PORT=$rPort
REDIS_DB=$rDb
"@
    Set-Content -LiteralPath $target -Value $content -Encoding ASCII
    Write-Host "Wrote server\.env (DB_TYPE=$dbType, generated SECRET_KEY, all keys populated)."
}

function Write-AgentConfig {
    $serverTarget = Join-Path $RootDir "agent\conf\server.json"
    $twTarget     = Join-Path $RootDir "agent\conf\time_weaver.json"

    if ((Test-Path -LiteralPath $serverTarget) -and (Test-Path -LiteralPath $twTarget) -and -not $Reconfigure) {
        Write-Host "Using existing agent config (pass -Reconfigure to regenerate)."
        return
    }

    # The agent connects to the shared MySQL database (it ships MySQL SQL only).
    $h  = if ($DbHost -ne "") { $DbHost } else { Ask "Agent DB host" "127.0.0.1" }
    $p  = if ($DbPort -ne "") { [int]$DbPort } else { [int](Ask "Agent DB port" "3306") }
    $u  = if ($DbUser -ne "") { $DbUser } else { Ask "Agent DB user" "timeweaver" }
    $pw = if ($DbPassword -ne "") { $DbPassword } else { Ask "Agent DB password" "" }
    $db = if ($DbName -ne "") { $DbName } else { Ask "Agent DB name" "timeweaver" }
    $sc = if ($DbSchema -ne "") { $DbSchema } else { Ask "Agent DB schema (blank if none)" "" }
    $dev = if ($DeviceName -ne "") { $DeviceName } else { Ask "Agent device name" $env:COMPUTERNAME }

    $level = if ($LogLevel -ne "") { $LogLevel } else { "debug" }
    $rmin  = if ($RescheduleMinute -ne "") { $RescheduleMinute } else { "*/5" }

    $cfg = Get-Content -LiteralPath (Join-Path $RootDir "agent\conf\server.sample.json") -Raw | ConvertFrom-Json
    $my = $cfg.databases.time_weaver.database.mysql
    $my.host = $h; $my.port = $p; $my.user = $u; $my.password = $pw; $my.database = $db; $my.schema = $sc
    $cfg.log.base.level = $level; $cfg.log.console.level = $level; $cfg.log.file_timed.level = $level
    ($cfg | ConvertTo-Json -Depth 30) | Set-Content -LiteralPath $serverTarget -Encoding ASCII
    Write-Host "Wrote agent\conf\server.json (host=$h db=$db, log level=$level)."

    $tw = Get-Content -LiteralPath (Join-Path $RootDir "agent\conf\time_weaver.sample.json") -Raw | ConvertFrom-Json
    $tw.device = $dev
    $tw.reschedule.minute = $rmin
    ($tw | ConvertTo-Json -Depth 30) | Set-Content -LiteralPath $twTarget -Encoding ASCII
    Write-Host "Wrote agent\conf\time_weaver.json (device=$dev, reschedule minute=$rmin)."
    Write-Host "  The agent registers this device automatically on first run (no manual DB seeding)."
}

function Write-ClientConfig {
    $target = Join-Path $RootDir "client\config.js"
    if ((Test-Path -LiteralPath $target) -and -not $Reconfigure) {
        Write-Host "Using existing client\config.js (pass -Reconfigure to regenerate)."
        return
    }
    $url = if ($ApiUrl -ne "") { $ApiUrl } else { Ask "API server URL" "http://127.0.0.1:8000/time_weaver" }
    $content = @"
const config = {
    API_SERVER_URL: "$url"
};

export default config;
"@
    Set-Content -LiteralPath $target -Value $content -Encoding ASCII
    Write-Host "Wrote client\config.js (API_SERVER_URL=$url)."
}

# --- Run ----------------------------------------------------------------------
if ($DoServer) { New-Item -ItemType Directory -Force -Path (Join-Path $RootDir "server\log") | Out-Null }
if ($DoAgent)  { New-Item -ItemType Directory -Force -Path (Join-Path $RootDir "agent\log")  | Out-Null }

if ($DoServer) {
    Setup-PythonProject (Join-Path $RootDir "server")
    Write-ServerEnv
}

if ($DoAgent) {
    Setup-PythonProject (Join-Path $RootDir "agent")
    Write-AgentConfig
}

if ($DoClient) {
    Push-Location (Join-Path $RootDir "client")
    try {
        npm ci
        Write-ClientConfig
        if (-not $SkipUiBuild) { npm run build }
    } finally {
        Pop-Location
    }
}

if ($DoServer) {
    Write-CommandFile (Join-Path $RootDir "run-server.cmd") @"
@echo off
cd /d "%~dp0server"
".venv\Scripts\python.exe" -m uvicorn app:app --host 0.0.0.0 --port 8000 --workers 1
"@
}

if ($DoAgent) {
    Write-CommandFile (Join-Path $RootDir "run-agent.cmd") @"
@echo off
cd /d "%~dp0agent"
".venv\Scripts\python.exe" timeweaver.py
"@
}

Write-Host ""
Write-Host "TimeWeaver Windows setup complete (component: $Component)."
Write-Host "Config was written automatically - no files to copy or edit."
Write-Host "Start:"
if ($DoServer) { Write-Host "  - run-server.cmd" }
if ($DoAgent)  { Write-Host "  - run-agent.cmd  (needs the shared database reachable)" }
if ($DoClient) { Write-Host "  - serve client\dist, or 'npm run serve' for development" }
