<#
.SYNOPSIS
  Skrip pendaftar otomatis Windows Task Scheduler untuk SEO-Boost (100% Lokal).

.DESCRIPTION
  Mendaftarkan tugas berkala di Windows Task Scheduler menggunakan schtasks.exe
  untuk menjalankan pipeline audit SEO secara otomatis setiap minggu.

.PARAMETER TaskName
  Nama tugas di Windows Task Scheduler (default: "SEOBoost_WeeklyAudit").

.PARAMETER Day
  Hari eksekusi mingguan: MON, TUE, WED, THU, FRI, SAT, SUN (default: "MON").

.PARAMETER Time
  Waktu eksekusi dalam format 24 jam HH:mm (default: "07:00").

.PARAMETER Site
  Nama situs di config.json atau "all" untuk semua situs (default: "all").

.PARAMETER Format
  Format laporan keluaran: text, md, html, json (default: "md").

.PARAMETER WhatIf
  Simulasikan perintah pendaftaran tanpa mengubah pengaturan sistem.
#>

param(
    [string]$TaskName = "SEOBoost_WeeklyAudit",
    [string]$Day = "MON",
    [string]$Time = "07:00",
    [string]$Site = "all",
    [string]$Format = "md",
    [switch]$WhatIf
)

$ErrorActionPreference = "Stop"

# Tentukan lokasi direktori root dan berkas biner
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path

$ExePath = Join-Path $ProjectRoot "seo-boost.exe"
$ConfigPath = Join-Path $ProjectRoot "config.json"
$ReportsDir = Join-Path $ProjectRoot "reports"
$OutputFile = Join-Path $ReportsDir "weekly_report.$Format"

# Pastikan folder reports ada
if (-not (Test-Path $ReportsDir) -and -not $WhatIf) {
    New-Item -ItemType Directory -Path $ReportsDir -Force | Out-Null
}

# Susun perintah eksekusi yang akan dijalankan oleh Task Scheduler
$TaskRun = "`"$ExePath`" run --config `"$ConfigPath`" --site $Site --format $Format --out `"$OutputFile`""

Write-Host "================================================================================"
Write-Host "  SEO-BOOST: SETUP WINDOWS TASK SCHEDULER (100% LOKAL)"
Write-Host "================================================================================"
Write-Host ""
Write-Host "Konfigurasi Jadwal:"
Write-Host "  Nama Task    : $TaskName"
Write-Host "  Jadwal       : Setiap $Day pukul $Time WIB"
Write-Host "  Target Site  : $Site"
Write-Host "  Format       : $Format"
Write-Host "  Biner Tool   : $ExePath"
Write-Host "  Output File  : $OutputFile"
Write-Host ""

$SchtasksArgs = @(
    "/Create",
    "/SC", "WEEKLY",
    "/D", $Day,
    "/ST", $Time,
    "/TN", $TaskName,
    "/TR", $TaskRun,
    "/F"
)

if ($WhatIf) {
    Write-Host "[WHATIF] Simulasi aktif. Perintah schtasks yang akan dieksekusi:"
    Write-Host "  schtasks.exe $($SchtasksArgs -join ' ')"
    Write-Host ""
    Write-Host "[OK] Simulasi selesai tanpa mengubah sistem."
    exit 0
}

# Verifikasi keberadaan berkas biner sebelum mendaftarkan task sungguhan
if (-not (Test-Path $ExePath)) {
    Write-Warning "[WARN] Berkas biner seo-boost.exe belum ditemukan di $ExePath."
    Write-Warning "       Silakan kompilasi terlebih dahulu dengan: go build -o seo-boost.exe ./cmd/seo-boost"
}

try {
    & schtasks.exe $SchtasksArgs
    if ($LASTEXITCODE -eq 0) {
        Write-Host ""
        Write-Host "[OK] Task jadwal '$TaskName' berhasil didaftarkan ke Windows Task Scheduler."
        Write-Host "     Audit SEO akan berjalan otomatis setiap $Day pukul $Time."
        Write-Host "     Hasil laporan akan disimpan di: $OutputFile"
    } else {
        Write-Error "[GAGAL] schtasks mengembalikan kode keluar: $LASTEXITCODE"
    }
} catch {
    Write-Error "[GAGAL] Gagal mendaftarkan task ke Windows Task Scheduler: $_"
}
