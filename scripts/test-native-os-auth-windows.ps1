# Run only on the disposable GitHub Windows runner. No logon policy changes.
$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_OS -ne 'Windows') {
    throw 'Native OS authentication fixtures require a disposable Windows CI runner.'
}

$nativeSuffix = [guid]::NewGuid().ToString('N').Substring(0, 6)
$nativePassword = 'Tw!' + [guid]::NewGuid().ToString('N') + 'aA9'
Write-Output "::add-mask::$nativePassword"
$nativeSecret = ConvertTo-SecureString $nativePassword -AsPlainText -Force
$nativeUsers = [System.Collections.Generic.List[string]]::new()
$nativeTemp = Join-Path $env:RUNNER_TEMP ('timeweaver-os-auth-' + [guid]::NewGuid().ToString('N'))
$nativeActive = 'twok' + $nativeSuffix
$nativeDisabled = 'twlock' + $nativeSuffix
$nativeExpired = 'twexpire' + $nativeSuffix
$nativePasswordExpired = 'twpw' + $nativeSuffix

try {
    New-Item -ItemType Directory -Path $nativeTemp | Out-Null
    foreach ($nativeUser in @($nativeActive, $nativeDisabled, $nativeExpired, $nativePasswordExpired)) {
        New-LocalUser -Name $nativeUser -Password $nativeSecret -AccountNeverExpires | Out-Null
        $nativeUsers.Add($nativeUser)
        Add-LocalGroupMember -SID 'S-1-5-32-545' -Member $nativeUser
    }
    Disable-LocalUser -Name $nativeDisabled
    Set-LocalUser -Name $nativeExpired -AccountExpires (Get-Date).AddDays(-2)
    Set-LocalUser -Name $nativePasswordExpired -PasswordNeverExpires $false
    # Use the documented WinNT PasswordExpired property rather than a CLI
    # password argument or changes to the runner's global password policy.
    $nativeEntry = [ADSI]("WinNT://$env:COMPUTERNAME/$nativePasswordExpired,user")
    try {
        $nativeEntry.Put('PasswordExpired', [int]1)
        $nativeEntry.SetInfo()
        if ([int]$nativeEntry.Get('PasswordExpired') -eq 0) {
            throw 'Could not configure the password-expired native fixture.'
        }
    }
    finally { $nativeEntry.Dispose() }

    $env:TIMEWEAVER_OS_AUTH_TEST_USER = $nativeActive
    $env:TIMEWEAVER_OS_AUTH_TEST_PASSWORD = $nativePassword
    $env:TIMEWEAVER_OS_AUTH_TEST_DISABLED = $nativeDisabled
    $env:TIMEWEAVER_OS_AUTH_TEST_EXPIRED = $nativeExpired
    $env:TIMEWEAVER_OS_AUTH_TEST_PASSWORD_EXPIRED = $nativePasswordExpired
    $env:CGO_ENABLED = '0'
    $nativeBinary = Join-Path $nativeTemp 'access-native.test.exe'
    & go test -c -o $nativeBinary ./internal/access
    if ($LASTEXITCODE -ne 0) { throw 'Native authentication test build failed.' }
    # Quote dotted Go flags as complete native arguments: when the command is
    # a variable, PowerShell can otherwise split -test.run into -test and .run.
    $nativeArgs = @('-test.run=^(TestNativeOS|TestLiveNativeOS)', '-test.v', '-test.timeout=2m')
    & $nativeBinary @nativeArgs
    if ($LASTEXITCODE -ne 0) { throw 'Native Windows authentication checks failed.' }
}
finally {
    foreach ($nativeUser in $nativeUsers) {
        Remove-LocalUser -Name $nativeUser -ErrorAction Continue
    }
    if (Test-Path $nativeTemp) { Remove-Item -LiteralPath $nativeTemp -Recurse -Force }
    Remove-Item Env:TIMEWEAVER_OS_AUTH_TEST_PASSWORD -ErrorAction SilentlyContinue
    $nativeSecret.Dispose()
}
