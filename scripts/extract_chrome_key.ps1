Add-Type -AssemblyName System.Security

$localState = "$env:LOCALAPPDATA\Google\Chrome\User Data\Local State"
if (-not (Test-Path $localState)) {
    $localState = "$env:LOCALAPPDATA\Microsoft\Edge\User Data\Local State"
}
if (-not (Test-Path $localState)) {
    Write-Host "Local State not found." -ForegroundColor Red
    exit 1
}

$json = Get-Content $localState -Raw | ConvertFrom-Json
$encKey = [Convert]::FromBase64String($json.os_crypt.encrypted_key)
$rawKey = $encKey[5..($encKey.Length - 1)]
$masterKey = [System.Security.Cryptography.ProtectedData]::Unprotect(
    $rawKey,
    $null,
    [System.Security.Cryptography.DataProtectionScope]::CurrentUser
)
$b64 = [Convert]::ToBase64String($masterKey)

# 1. Save to Windows UserProfile (.openusage_chrome_key)
$userDest = "$env:USERPROFILE\.openusage_chrome_key"
Set-Content -Path $userDest -Value $b64 -Encoding ascii -NoNewline

# 2. Also try writing to WSL directly
$dest = "\\wsl$\Ubuntu\home\nurul\.config\openusage\chrome_key"
if (-not (Test-Path "\\wsl$\Ubuntu\home\nurul\.config\openusage")) {
    $dest = "\\wsl.localhost\Ubuntu\home\nurul\.config\openusage\chrome_key"
}
if (Test-Path (Split-Path $dest -Parent)) {
    Set-Content -Path $dest -Value $b64 -Encoding ascii -NoNewline
}

Write-Host "✓ Successfully exported browser key to OpenUsage!" -ForegroundColor Green
