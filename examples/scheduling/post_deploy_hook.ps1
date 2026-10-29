<#
.SYNOPSIS
  Skrip hook pasca-deploy artikel baru untuk Windows (PowerShell).

.DESCRIPTION
  Memicu pengiriman sitemap ke Google Search Console dan ping IndexNow segera
  setelah konten atau artikel baru selesai dipublikasikan/dideploy.

.PARAMETER Site
  Nama situs target di config.json (default: "all").

.PARAMETER DryRun
  Simulasikan pengiriman tanpa memanggil API eksternal.
#>

param(
    [string]$Site = "all",
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
$ExePath = Join-Path $ProjectRoot "seo-boost.exe"
$ConfigPath = Join-Path $ProjectRoot "config.json"

Write-Host "================================================================================"
Write-Host "  SEO-BOOST: POST-DEPLOY SUBMIT HOOK"
Write-Host "================================================================================"
Write-Host "Target Site : $Site"
Write-Host "Mode        : $(if ($DryRun) { '[DRY RUN] Simulasi' } else { '[LIVE] Pengiriman Aktif' })"
Write-Host ""

$SubmitArgs = @(
    "submit",
    "--config", $ConfigPath,
    "--site", $Site,
    "--gsc",
    "--indexnow"
)

if ($DryRun) {
    $SubmitArgs += "--dry-run"
}

if (-not (Test-Path $ExePath)) {
    Write-Host "[INFO] Menjalankan via 'go run' karena biner belum dikompilasi..."
    Push-Location $ProjectRoot
    try {
        go run .\cmd\seo-boost @SubmitArgs
    } finally {
        Pop-Location
    }
} else {
    & $ExePath $SubmitArgs
}
