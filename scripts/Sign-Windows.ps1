# Created by nc.ai 0.8.8.5 on new-admin-pc 2026-10-19 11:41:20
<#
.SYNOPSIS
  Подписывает NotCursor.exe самоподписанным (self-signed) Code Signing сертификатом.

.DESCRIPTION
  Публичная подпись без покупки CA-сертификата не даёт репутации SmartScreen, но
  делает бинарь «доверенным издателем» на машинах, где установлен этот корневой
  сертификат (свой парк). Это убирает предупреждения Defender/SmartScreen локально.

  Сертификат создаётся один раз в Cert:\CurrentUser\My и переиспользуется.

.PARAMETER Path
  Файл .exe/.msi для подписи. По умолчанию build\bin\NotCursor.exe.

.PARAMETER ExportCer
  Куда выгрузить .cer для установки в «Доверенные корневые» на целевых машинах.

.PARAMETER TrustLocal
  Сразу установить сертификат в Cert:\CurrentUser\Root на этой машине.
#>
param(
  [string]$Path = 'build\bin\NotCursor.exe',
  [string]$ExportCer = 'build\dist\NotCursor-selfsigned.cer',
  [switch]$TrustLocal
)

$ErrorActionPreference = 'Stop'

$subject = 'CN=NotCursor.ai (self-signed)'
$cert = Get-ChildItem Cert:\CurrentUser\My |
  Where-Object { $_.Subject -eq $subject -and $_.HasPrivateKey } |
  Sort-Object NotAfter -Descending |
  Select-Object -First 1

if (-not $cert) {
  Write-Host 'Создаю self-signed Code Signing сертификат...'
  $cert = New-SelfSignedCertificate `
    -Type CodeSigningCert `
    -Subject $subject `
    -KeyUsage DigitalSignature `
    -KeyExportPolicy Exportable `
    -KeyLength 3072 `
    -HashAlgorithm SHA256 `
    -CertStoreLocation 'Cert:\CurrentUser\My' `
    -NotAfter (Get-Date).AddYears(5)
}

if (-not (Test-Path -LiteralPath $Path)) {
  throw "Файл для подписи не найден: $Path"
}

$dir = Split-Path -Parent $ExportCer
if ($dir -and -not (Test-Path -LiteralPath $dir)) {
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
}
Export-Certificate -Cert $cert -FilePath $ExportCer -Force | Out-Null
Write-Host "Публичная часть сертификата: $ExportCer"

$sig = Set-AuthenticodeSignature -FilePath $Path -Certificate $cert -HashAlgorithm SHA256 -TimestampServer 'http://timestamp.digicert.com'
if ($sig.Status -ne 'Valid') {
  Write-Warning ("Статус подписи: {0} — {1}" -f $sig.Status, $sig.StatusMessage)
} else {
  Write-Host "Подписано: $Path"
}
Get-AuthenticodeSignature -FilePath $Path | Format-List Status, StatusMessage, SignerCertificate

if ($TrustLocal) {
  Import-Certificate -FilePath $ExportCer -CertStoreLocation 'Cert:\CurrentUser\Root' | Out-Null
  Write-Host 'Сертификат добавлен в Cert:\CurrentUser\Root (доверенный на этой машине).'
}
