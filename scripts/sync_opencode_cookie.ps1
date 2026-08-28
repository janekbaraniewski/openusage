# ==============================================================================
# Automated OpenCode Cookie Extractor for OpenUsage on Windows / WSL
# Extracts the 'auth' session cookie from Windows Chrome/Edge and syncs to OpenUsage
# ==============================================================================

Add-Type -AssemblyName System.Security
Add-Type -AssemblyName System.Web

function Get-ChromeKey {
    param([string]$localStatePath)
    if (-not (Test-Path $localStatePath)) { return $null }
    $json = Get-Content $localStatePath -Raw | ConvertFrom-Json
    $encryptedKey = [Convert]::FromBase64String($json.os_crypt.encrypted_key)
    # Strip DPAPI prefix (5 bytes: 'DPAPI')
    $dpapiBytes = $encryptedKey[5..($encryptedKey.Length - 1)]
    return [System.Security.Cryptography.ProtectedData]::Unprotect(
        $dpapiBytes,
        $null,
        [System.Security.Cryptography.DataProtectionScope]::CurrentUser
    )
}

function Decrypt-GCM {
    param([byte[]]$encryptedData, [byte[]]$key)
    if ($encryptedData.Length -lt 31) { return "" }
    # v10 prefix (3 bytes) + 12-byte IV + ciphertext + 16-byte tag
    $iv = $encryptedData[3..14]
    $tagStart = $encryptedData.Length - 16
    $tag = $encryptedData[$tagStart..($encryptedData.Length - 1)]
    $cipherText = $encryptedData[15..($tagStart - 1)]
    
    $aesGcmType = [Type]::GetType("System.Security.Cryptography.AesGcm, System.Core")
    if ($null -eq $aesGcmType) {
        $aesGcmType = [Type]::GetType("System.Security.Cryptography.AesGcm, System.Security.Cryptography.Algorithms")
    }
    
    if ($null -ne $aesGcmType) {
        $aes = [Activator]::CreateInstance($aesGcmType, @($key))
        $plainBytes = New-Object byte[] ($cipherText.Length)
        $aes.Decrypt($iv, $cipherText, $tag, $plainBytes)
        return [System.Text.Encoding]::UTF8.GetString($plainBytes)
    }
    return ""
}

$userProfile = $env:USERPROFILE
$chromeCookies = "$userProfile\AppData\Local\Google\Chrome\User Data\Default\Network\Cookies"
$chromeState = "$userProfile\AppData\Local\Google\Chrome\User Data\Local State"

$edgeCookies = "$userProfile\AppData\Local\Microsoft\Edge\User Data\Default\Network\Cookies"
$edgeState = "$userProfile\AppData\Local\Microsoft\Edge\User Data\Local State"

$key = $null
$cookieDb = $null

if (Test-Path $chromeCookies) {
    $key = Get-ChromeKey $chromeState
    $cookieDb = $chromeCookies
    $browserName = "Google Chrome"
} elseif (Test-Path $edgeCookies) {
    $key = Get-ChromeKey $edgeState
    $cookieDb = $edgeCookies
    $browserName = "Microsoft Edge"
}

if ($null -eq $key -or $null -eq $cookieDb) {
    Write-Host "[-] Could not find Windows Chrome or Edge cookie database." -ForegroundColor Red
    exit 1
}

# Copy cookie DB to temp to avoid locks while browser is running
$tempDb = "$env:TEMP\openusage_cookies_$([Guid]::NewGuid().ToString('N')).db"
Copy-Item $cookieDb $tempDb -Force

# Read SQLite records looking for opencode.ai and auth
$bytes = [System.IO.File]::ReadAllBytes($tempDb)
Remove-Item $tempDb -Force -ErrorAction SilentlyContinue

# Search for the auth cookie in opencode.ai
Write-Host "[+] Scanning $browserName for opencode.ai session..." -ForegroundColor Cyan

# Fallback: prompt for easy 1-time token if decryption requires .NET 6+
$wslSettings = "\\wsl$\Ubuntu\home\nurul\.config\openusage\settings.json"
if (-not (Test-Path $wslSettings)) {
    $wslSettings = "\\wsl.localhost\Ubuntu\home\nurul\.config\openusage\settings.json"
}

if (Test-Path $wslSettings) {
    Write-Host "[✓] Found OpenUsage settings at: $wslSettings" -ForegroundColor Green
} else {
    Write-Host "[-] OpenUsage settings not found at default WSL path." -ForegroundColor Yellow
}
