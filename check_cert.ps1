$certPath = "$env:APPDATA\tergum\certs\ca.crt"
if (Test-Path $certPath) {
    $cert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2($certPath)
    $hashBytes = $cert.GetCertHash([System.Security.Cryptography.HashAlgorithmName]::SHA256)
    $hashStr = [BitConverter]::ToString($hashBytes) -replace '-', ':'
    Write-Output $hashStr
} else {
    Write-Output "File not found at $certPath"
}
