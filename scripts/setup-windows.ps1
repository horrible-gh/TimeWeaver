param(
    [string]$Python = "python",
    [ValidateSet("", "all", "server", "client")]
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
    # Client config
    [string]$ApiUrl = "",
    # Behaviour
    [switch]$NonInteractive,
    [switch]$Reconfigure,
    [switch]$SkipUiBuild,
    # Register the legacy Python server to start on boot (scheduled task running as SYSTEM).
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
    # Use the instance API (Create().GetBytes()) rather than the static
    # RandomNumberGenerator::Fill(Span<byte>): Fill only exists on .NET Core 3.0 /
    # .NET 5+, so it throws MethodNotFound under Windows PowerShell 5.1 (which runs
    # on .NET Framework). Create()+GetBytes() exist on BOTH runtimes (and pwsh 7+),
    # with identical cryptographic strength and output.
    $bytes = New-Object byte[] 32
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
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


# --- Component selection ------------------------------------------------------
if ($Component -eq "") {
    $Component = Ask "Install which component? (all/server/client)" "all"
}
if ($Component -notin @("all", "server", "client")) {
    Write-Error "Invalid component: $Component (expected all|server|client). Install the Go agent separately: https://github.com/horrible-gh/TimeWeaver-Agent"
    exit 2
}
$DoServer = $Component -in @("all", "server")
$DoClient = $Component -in @("all", "client")

# --- Server database resolution -----------------------------------------------
# Database selection and credentials belong to the server only.
$script:DbType = $DbType
$script:My = @{ host = ""; port = ""; user = ""; password = ""; database = ""; schema = "" }

function Resolve-SharedDb {
    $existingEnv = Read-EnvFile (Join-Path $RootDir "server\.env")

    if ($DoServer) {
        $seedType = if ($existingEnv.ContainsKey("DB_TYPE") -and $existingEnv["DB_TYPE"]) { $existingEnv["DB_TYPE"] } else { "sqlite3" }
        $script:DbType = Pick $DbType "Server database type (sqlite3/mysql)" $seedType "sqlite3"
    }

    # Collect MySQL connection details only when the server uses MySQL.
    if ($DoServer -and $script:DbType -eq "mysql") {
        $script:My.host     = Pick $DbHost     "DB host"     $(if ($existingEnv["DB_HOST"]) { $existingEnv["DB_HOST"] } else { "" }) "127.0.0.1"
        $script:My.port     = Pick $DbPort     "DB port"     $(if ($existingEnv["DB_PORT"]) { $existingEnv["DB_PORT"] } else { "" }) "3306"
        $script:My.user     = Pick $DbUser     "DB user"     $(if ($existingEnv["DB_USER"]) { $existingEnv["DB_USER"] } else { "" }) "timeweaver"
        $script:My.password = Pick $DbPassword "DB password" $(if ($existingEnv["DB_PASSWORD"]) { $existingEnv["DB_PASSWORD"] } else { "" }) ""
        $script:My.database = Pick $DbName     "DB name"     $(if ($existingEnv["DB_DATABASE"]) { $existingEnv["DB_DATABASE"] } else { "" }) "timeweaver"
        $script:My.schema   = Pick $DbSchema   "DB schema (blank if none)" $(if ($existingEnv["DB_SCHEMA"]) { $existingEnv["DB_SCHEMA"] } else { "" }) ""
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
if (-not $InstallServices -and -not $NonInteractive -and $DoServer) {
    $ans = Ask "Register TimeWeaver to start on boot (Windows scheduled task, runs as SYSTEM)? (y/N)" "N"
    if ($ans -match '^(y|yes)$') { $InstallServices = $true }
}

# --- Run ----------------------------------------------------------------------
if ($DoServer) { New-Item -ItemType Directory -Force -Path (Join-Path $RootDir "server\log") | Out-Null }

if ($DoServer) {
    Setup-PythonProject (Join-Path $RootDir "server")
    Write-ServerEnv
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
    Write-Host "Inspect them in Task Scheduler, or: schtasks /query /tn `"TimeWeaver Server`""
    if ($DoClient) { Write-Host "Client: serve client\dist, or 'npm run serve' for development" }
} else {
    Write-Host "Start:"
    if ($DoServer) { Write-Host "  - run-server.cmd  (listens on $($script:ServerHost):$($script:ServerPort))" }
    if ($DoClient) { Write-Host "  - serve client\dist, or 'npm run serve' for development" }
}
