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
    [string]$DbLog = "",
    [string]$SecretKey = "",
    [string]$AllowedOrigin = "",
    [string]$Context = "",
    [string]$AccessTokenExpireMinutes = "",
    [string]$ServerHost = "",
    [string]$ServerPort = "",
    [string]$RedisHost = "",
    [string]$RedisPort = "",
    [string]$RedisDb = "",
    # Agent config
    [string]$DeviceName = "",
    [string]$RescheduleYear = "",
    [string]$RescheduleMonth = "",
    [string]$RescheduleDay = "",
    [string]$RescheduleHour = "",
    [string]$RescheduleMinute = "",
    [string]$RescheduleSecond = "",
    [string]$LogLevel = "",
    # Client config
    [string]$ApiUrl = "",
    # Behaviour
    [switch]$NonInteractive,
    [switch]$Reconfigure,
    [switch]$SkipUiBuild,
    # Register server/agent to start on boot (scheduled task running as SYSTEM).
    # Interactive installs also OFFER this as a prompt, so the switch is only
    # needed for unattended/CI runs. Requires an elevated (Administrator) shell.
    [switch]$InstallServices
)

$ErrorActionPreference = "Stop"
$RootDir = Split-Path -Parent $PSScriptRoot

# NOTE: $NonInteractive is honoured ONLY when explicitly passed (or CI=true). The
# installer no longer auto-detects a redirected stdin: the whole point is that a
# plain interactive install lets you set every value, so we never silence the
# prompts behind your back. CI keeps a safe escape hatch via the env var.
if (-not $NonInteractive -and $env:CI) { $NonInteractive = $true }

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

function Backup-IfExists {
    param([string]$Path)
    if (Test-Path -LiteralPath $Path) {
        $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
        $backupDir = Join-Path $RootDir "backups\$stamp"
        New-Item -ItemType Directory -Force -Path $backupDir | Out-Null
        $dest = Join-Path $backupDir (Split-Path -Leaf $Path)
        Copy-Item -LiteralPath $Path -Destination $dest -Force
        Write-Host "  Backed up existing $(Split-Path -Leaf $Path) -> backups\$stamp\"
    }
}

function Test-Admin {
    $id = [System.Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object System.Security.Principal.WindowsPrincipal($id)
    return $principal.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)
}

# G2: Windows had no service support at all (Linux at least had a flag). Register
# the run-*.cmd launchers as scheduled tasks that start at boot under SYSTEM,
# with automatic restart - the native parity for systemd without bundling NSSM.
function Install-WindowsService {
    param([string]$TaskName, [string]$CmdPath, [string]$Description)
    $action  = New-ScheduledTaskAction -Execute "cmd.exe" -Argument "/c `"$CmdPath`""
    $trigger = New-ScheduledTaskTrigger -AtStartup
    $principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
    # No execution time limit (long-running), restart a few times on failure.
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
        -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit (New-TimeSpan -Seconds 0)
    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Principal $principal `
        -Settings $settings -Description $Description -Force | Out-Null
    Write-Host "Registered scheduled task '$TaskName' (starts at boot, runs as SYSTEM)."
}

# Read an existing server\.env into a hashtable so re-runs can offer current
# values as defaults instead of silently skipping the whole config step.
function Read-EnvFile {
    param([string]$Path)
    $map = @{}
    if (Test-Path -LiteralPath $Path) {
        foreach ($line in Get-Content -LiteralPath $Path) {
            if ($line -match '^\s*([A-Za-z0-9_]+)\s*=\s*(.*)$') {
                $map[$Matches[1]] = $Matches[2]
            }
        }
    }
    return $map
}

function Pick {
    # Resolve a value: explicit flag wins, else prompt (seeded with the existing
    # value when present, otherwise the built-in default).
    param([string]$Flag, [string]$Prompt, [string]$Existing, [string]$Default)
    if ($Flag -ne "") { return $Flag }
    $seed = if ($Existing -ne "" -and $null -ne $Existing) { $Existing } else { $Default }
    return Ask $Prompt $seed
}

# Read the existing agent MySQL block (agent\conf\server.json) into a hashtable.
# Empty values for missing keys; unfilled sample placeholders like "<DB_USER>"
# are treated as empty so they never become a seed.
function Read-AgentDbConfig {
    param([string]$Path)
    $map = @{ host = ""; port = ""; user = ""; password = ""; database = ""; schema = "" }
    if (-not (Test-Path -LiteralPath $Path)) { return $map }
    try {
        $cfg = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
        $my = $cfg.databases.time_weaver.database.mysql
        foreach ($k in @($map.Keys)) {
            $v = [string]$my.$k
            if ($v -like "<*>") { $v = "" }   # unfilled sample placeholder
            $map[$k] = $v
        }
    } catch { }
    return $map
}

# Seed for a shared-DB prompt: prefer server\.env, else fall back to the value
# already in agent\conf\server.json. This stops an agent-only re-install (which
# has no server\.env on this host) from resetting valid DB settings to defaults.
function Get-DbSeed {
    param([hashtable]$Env, [string]$EnvKey, [hashtable]$AgentDb, [string]$JsonKey)
    if ($Env.ContainsKey($EnvKey) -and $Env[$EnvKey]) { return $Env[$EnvKey] }
    return $AgentDb[$JsonKey]
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

# --- Shared database resolution ----------------------------------------------
# The agent ships MySQL SQL only; the server supports both. Resolve the DB once
# so an "all" install asks a single set of DB questions and the server + agent
# end up pointing at the SAME database. If the agent is involved but the server
# is on sqlite3, warn loudly (they would otherwise never share data).
$script:DbType = $DbType
$script:My = @{ host = ""; port = ""; user = ""; password = ""; database = ""; schema = "" }

function Resolve-SharedDb {
    $existingEnv = Read-EnvFile (Join-Path $RootDir "server\.env")
    $existingAgentDb = Read-AgentDbConfig (Join-Path $RootDir "agent\conf\server.json")

    if ($DoServer) {
        $seedType = if ($existingEnv.ContainsKey("DB_TYPE") -and $existingEnv["DB_TYPE"]) { $existingEnv["DB_TYPE"] } else { "sqlite3" }
        $script:DbType = Pick $DbType "Server database type (sqlite3/mysql)" $seedType "sqlite3"
    }

    if ($DoServer -and $DoAgent -and $script:DbType -ne "mysql") {
        Write-Host ""
        Write-Host "  [!] The agent works with MySQL ONLY, but the server DB is '$($script:DbType)'." -ForegroundColor Yellow
        Write-Host "      With different databases the agent cannot see the server's data." -ForegroundColor Yellow
        $switch = Ask "      Switch the whole stack to a shared MySQL? (Y/n)" "Y"
        if ($switch -match '^(y|yes)$') { $script:DbType = "mysql" }
        else { Write-Host "      Proceeding split: server on $($script:DbType), agent on its own MySQL." -ForegroundColor Yellow }
        Write-Host ""
    }

    # Collect MySQL connection details once if anything needs MySQL.
    if ($script:DbType -eq "mysql" -or $DoAgent) {
        $script:My.host     = Pick $DbHost     "DB host"     (Get-DbSeed $existingEnv "DB_HOST"     $existingAgentDb "host")     "127.0.0.1"
        $script:My.port     = Pick $DbPort     "DB port"     (Get-DbSeed $existingEnv "DB_PORT"     $existingAgentDb "port")     "3306"
        $script:My.user     = Pick $DbUser     "DB user"     (Get-DbSeed $existingEnv "DB_USER"     $existingAgentDb "user")     "timeweaver"
        $script:My.password = Pick $DbPassword "DB password" (Get-DbSeed $existingEnv "DB_PASSWORD" $existingAgentDb "password") ""
        $script:My.database = Pick $DbName     "DB name"     (Get-DbSeed $existingEnv "DB_DATABASE" $existingAgentDb "database") "timeweaver"
        $script:My.schema   = Pick $DbSchema   "DB schema (blank if none)" (Get-DbSeed $existingEnv "DB_SCHEMA" $existingAgentDb "schema") ""
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
    param([string]$Path, [string]$Content)
    Set-Content -LiteralPath $Path -Value $Content -Encoding ASCII
    Write-Host "Created $Path"
}

# --- Config generators (always enter config; existing values become defaults) -
function Write-ServerEnv {
    $target = Join-Path $RootDir "server\.env"
    $existing = Read-EnvFile $target

    $dbType = $script:DbType
    # SECRET_KEY: keep the existing one on re-run, else a fresh crypto-random
    # value (never a placeholder). A flag can pin it for reproducible deploys.
    $secret = if ($SecretKey -ne "") { $SecretKey }
              elseif ($existing.ContainsKey("SECRET_KEY") -and $existing["SECRET_KEY"]) { $existing["SECRET_KEY"] }
              else { New-SecretKey }

    $origin   = Pick $AllowedOrigin "CORS allowed origin (ALLOWED_ORIGIN)" $existing["ALLOWED_ORIGIN"] "*"
    $ctx      = Pick $Context "API context path (CONTEXT)" $existing["CONTEXT"] "/time_weaver"
    $expire   = Pick $AccessTokenExpireMinutes "Access token lifetime in minutes (ACCESS_TOKEN_EXPIRE_MINUTES)" $existing["ACCESS_TOKEN_EXPIRE_MINUTES"] "30"
    $dbLog    = Pick $DbLog "Log DB queries? (true/false; DB_LOG)" $existing["DB_LOG"] "true"
    $rHost    = Pick $RedisHost "Redis host (optional; REDIS_HOST)" $existing["REDIS_HOST"] "localhost"
    $rPort    = Pick $RedisPort "Redis port (REDIS_PORT)" $existing["REDIS_PORT"] "6379"
    $rDb      = Pick $RedisDb "Redis db index (REDIS_DB)" $existing["REDIS_DB"] "0"

    if ($dbType -eq "mysql") {
        $h = $script:My.host; $p = $script:My.port; $u = $script:My.user
        $pw = $script:My.password; $db = $script:My.database; $sc = $script:My.schema
        $dbPath = ""
    } else {
        $h = "127.0.0.1"; $p = "0"; $u = ""; $pw = ""; $db = ""; $sc = ""
        $dbPath = Pick $DbPath "SQLite file path (DB_PATH)" $existing["DB_PATH"] "./timeweaver.sqlite3"
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
DB_LOG=$dbLog
DB_PATH=$dbPath
REDIS_HOST=$rHost
REDIS_PORT=$rPort
REDIS_DB=$rDb
"@
    Backup-IfExists $target
    Set-Content -LiteralPath $target -Value $content -Encoding ASCII
    Write-Host "Wrote server\.env (DB_TYPE=$dbType, all keys populated)."
}

function Write-AgentConfig {
    $serverTarget = Join-Path $RootDir "agent\conf\server.json"
    $twTarget     = Join-Path $RootDir "agent\conf\time_weaver.json"

    # Existing values become the defaults so a re-run is non-destructive if you
    # just press Enter - but the config step ALWAYS runs (no silent skip).
    $exTw = $null
    if (Test-Path -LiteralPath $twTarget) { $exTw = Get-Content -LiteralPath $twTarget -Raw | ConvertFrom-Json }
    $exSrv = $null
    if (Test-Path -LiteralPath $serverTarget) { $exSrv = Get-Content -LiteralPath $serverTarget -Raw | ConvertFrom-Json }

    # The agent always talks to MySQL; reuse the shared DB resolved earlier.
    $h  = $script:My.host
    $p  = $script:My.port
    $u  = $script:My.user
    $pw = $script:My.password
    $db = $script:My.database
    $sc = $script:My.schema

    $exDev = if ($exTw) { [string]$exTw.device } else { "" }
    $dev = Pick $DeviceName "Agent device name" $exDev $env:COMPUTERNAME

    $exLevel = if ($exSrv) { [string]$exSrv.log.base.level } else { "" }
    $level = Pick $LogLevel "Agent log level (debug/info/warning/error)" $exLevel "debug"

    # Full reschedule cron is configurable here - not just the minute field.
    $exRe = if ($exTw) { $exTw.reschedule } else { $null }
    $ry = Pick $RescheduleYear   "Reschedule cron - year"   ($(if($exRe){[string]$exRe.year})  ) "*"
    $rmo= Pick $RescheduleMonth  "Reschedule cron - month"  ($(if($exRe){[string]$exRe.month}) ) "*"
    $rd = Pick $RescheduleDay    "Reschedule cron - day"    ($(if($exRe){[string]$exRe.day})   ) "*"
    $rh = Pick $RescheduleHour   "Reschedule cron - hour"   ($(if($exRe){[string]$exRe.hour})  ) "*"
    $rmi= Pick $RescheduleMinute "Reschedule cron - minute" ($(if($exRe){[string]$exRe.minute})) "*/5"
    $rs = Pick $RescheduleSecond "Reschedule cron - second" ($(if($exRe){[string]$exRe.second})) "0"

    $cfg = Get-Content -LiteralPath (Join-Path $RootDir "agent\conf\server.sample.json") -Raw | ConvertFrom-Json
    $my = $cfg.databases.time_weaver.database.mysql
    $my.host = $h; $my.port = [int]$p; $my.user = $u; $my.password = $pw; $my.database = $db; $my.schema = $sc
    $cfg.log.base.level = $level; $cfg.log.console.level = $level; $cfg.log.file_timed.level = $level
    Backup-IfExists $serverTarget
    ($cfg | ConvertTo-Json -Depth 30) | Set-Content -LiteralPath $serverTarget -Encoding ASCII
    Write-Host "Wrote agent\conf\server.json (host=$h db=$db, log level=$level)."

    $tw = Get-Content -LiteralPath (Join-Path $RootDir "agent\conf\time_weaver.sample.json") -Raw | ConvertFrom-Json
    $tw.device = $dev
    $tw.reschedule.year = $ry; $tw.reschedule.month = $rmo; $tw.reschedule.day = $rd
    $tw.reschedule.hour = $rh; $tw.reschedule.minute = $rmi; $tw.reschedule.second = $rs
    Backup-IfExists $twTarget
    ($tw | ConvertTo-Json -Depth 30) | Set-Content -LiteralPath $twTarget -Encoding ASCII
    Write-Host "Wrote agent\conf\time_weaver.json (device=$dev, reschedule=$ry $rmo $rd $rh $rmi $rs)."
    Write-Host "  The agent registers this device automatically on first run (no manual DB seeding)."
}

function Write-ClientConfig {
    $target = Join-Path $RootDir "client\config.js"
    $existingUrl = ""
    if (Test-Path -LiteralPath $target) {
        $m = Select-String -LiteralPath $target -Pattern 'API_SERVER_URL:\s*"([^"]*)"' | Select-Object -First 1
        if ($m) { $existingUrl = $m.Matches[0].Groups[1].Value }
    }
    # Default the client URL to the server bind we just chose.
    $defUrl = "http://127.0.0.1:$($script:ServerPort)$($script:ServerCtx)"
    $url = Pick $ApiUrl "API server URL" $existingUrl $defUrl
    $content = @"
const config = {
    API_SERVER_URL: "$url"
};

export default config;
"@
    Backup-IfExists $target
    Set-Content -LiteralPath $target -Value $content -Encoding ASCII
    Write-Host "Wrote client\config.js (API_SERVER_URL=$url)."
}

# --- Resolve cross-cutting values up front -----------------------------------
Resolve-SharedDb

# Server bind host/port are real install settings (no longer hard-coded in
# run-server.cmd). The client default URL derives from them.
$existingEnvTop = Read-EnvFile (Join-Path $RootDir "server\.env")
$script:ServerHost = Pick $ServerHost "Server bind host" "" "0.0.0.0"
$script:ServerPort = Pick $ServerPort "Server bind port" "" "8000"
$script:ServerCtx  = if ($Context -ne "") { $Context }
                     elseif ($existingEnvTop.ContainsKey("CONTEXT") -and $existingEnvTop["CONTEXT"]) { $existingEnvTop["CONTEXT"] }
                     else { "/time_weaver" }

# G1: surface the service question in interactive installs instead of hiding it
# behind a switch. The -InstallServices switch or CI keep unattended runs as-is.
if (-not $InstallServices -and -not $NonInteractive -and ($DoServer -or $DoAgent)) {
    $ans = Ask "Register TimeWeaver to start on boot (Windows scheduled task, runs as SYSTEM)? (y/N)" "N"
    if ($ans -match '^(y|yes)$') { $InstallServices = $true }
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
".venv\Scripts\python.exe" -m uvicorn app:app --host $($script:ServerHost) --port $($script:ServerPort) --workers 1
"@
}

if ($DoAgent) {
    Write-CommandFile (Join-Path $RootDir "run-agent.cmd") @"
@echo off
cd /d "%~dp0agent"
".venv\Scripts\python.exe" timeweaver.py
"@
}

# --- Service registration (scheduled tasks) ----------------------------------
$servicesInstalled = $false
if ($InstallServices) {
    if (-not (Test-Admin)) {
        # G4 (Windows): don't fail a finished install; tell the user how to add
        # the tasks from an elevated shell in a second pass.
        Write-Host ""
        Write-Host "  [!] Registering a Windows scheduled task needs an elevated (Administrator) PowerShell." -ForegroundColor Yellow
        Write-Host "      Everything else is done. To add the auto-start tasks, re-run from an elevated prompt:" -ForegroundColor Yellow
        Write-Host "        .\install.ps1 -Component $Component -InstallServices -NonInteractive" -ForegroundColor Yellow
    } else {
        if ($DoServer) {
            Install-WindowsService "TimeWeaver Server" (Join-Path $RootDir "run-server.cmd") "TimeWeaver FastAPI server (auto-start at boot)."
            $servicesInstalled = $true
        }
        if ($DoAgent) {
            Install-WindowsService "TimeWeaver Agent" (Join-Path $RootDir "run-agent.cmd") "TimeWeaver scheduler agent (auto-start at boot)."
            $servicesInstalled = $true
        }
    }
}

Write-Host ""
Write-Host "TimeWeaver Windows setup complete (component: $Component)."
Write-Host "Config was written automatically - no files to copy or edit."
# G3: advertise the scheduled tasks when they exist, not a foreground command
# that makes manual running look like the only option.
if ($servicesInstalled) {
    Write-Host "Services were registered as scheduled tasks (start at boot, run as SYSTEM)."
    Write-Host "Start them now without rebooting:"
    if ($DoServer) { Write-Host "  schtasks /run /tn `"TimeWeaver Server`"" }
    if ($DoAgent)  { Write-Host "  schtasks /run /tn `"TimeWeaver Agent`"" }
    Write-Host "Inspect them in Task Scheduler, or: schtasks /query /tn `"TimeWeaver Server`""
    if ($DoClient) { Write-Host "Client: serve client\dist, or 'npm run serve' for development" }
} else {
    Write-Host "Start:"
    if ($DoServer) { Write-Host "  - run-server.cmd  (listens on $($script:ServerHost):$($script:ServerPort))" }
    if ($DoAgent)  { Write-Host "  - run-agent.cmd  (needs the shared database reachable)" }
    if ($DoClient) { Write-Host "  - serve client\dist, or 'npm run serve' for development" }
}
