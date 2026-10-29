@echo off
setlocal enabledelayedexpansion
title SEO Boost - Local Web Dashboard

echo ========================================================
echo   SEO Boost - Local Web Dashboard
echo ========================================================
echo.

:: 1. Periksa ketersediaan file executable seo-boost.exe
if not exist "seo-boost.exe" (
    echo [WARN] File seo-boost.exe belum ditemukan.
    where go >nul 2>nul
    if !errorlevel! neq 0 (
        echo [ERR] Kompiler Go tidak terdeteksi di sistem PATH.
        echo [INFO] Silakan install Go terlebih dahulu dari https://go.dev/dl/
        echo.
        pause
        exit /b 1
    )
    echo [INFO] Kompiler Go terdeteksi di komputer Anda.
    echo [RUN] Mengompilasi seo-boost.exe secara otomatis...
    go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o seo-boost.exe ./cmd/seo-boost
    if !errorlevel! neq 0 (
        echo.
        echo [ERR] Kompilasi gagal! Silakan periksa pesan error di atas.
        echo.
        pause
        exit /b 1
    )
    echo [OK] Kompilasi berhasil! File seo-boost.exe siap digunakan.
    echo.
)

:: 2. Periksa ketersediaan config.json
if not exist "config.json" (
    if exist "config.example.json" (
        echo [WARN] config.json belum ditemukan.
        echo [INFO] Menyalin konfigurasi awal dari config.example.json...
        copy config.example.json config.json >nul
        echo [OK] config.json berhasil dibuat.
        echo.
    ) else (
        echo [ERR] File config.json dan config.example.json tidak ditemukan!
        echo.
        pause
        exit /b 1
    )
)

:: 3. Jalankan server lokal dan buka browser
echo [INFO] Menjalankan server dan membuka browser...
echo [INFO] Tekan Ctrl+C di jendela ini untuk mematikan server.
echo.

.\seo-boost.exe serve --open
pause
