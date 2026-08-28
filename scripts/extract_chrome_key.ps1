Add-Type -AssemblyName System.Security
$localState = "$env:LOCALAPPDATA\Google\Chrome\User Data\Local State"
if (-not (Test-Path $localState)) {
    Write-Host "Chrome Local State not found." -ForegroundColor Red
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
$dest = "\\wsl$\Ubuntu\home\nurul\.config\openusage\chrome_key"
Set-Content -Path $dest -Value $b64 -Encoding ascii -NoNewline
Write-Host "✓ Successfully exported Chrome key to OpenUsage!" -ForegroundColor Green
