@echo off
title SEO Boost Web Dashboard
echo ========================================================
echo   SEO Boost - Local Web Dashboard
echo ========================================================
echo [INFO] Menjalankan server dan membuka browser...
echo [INFO] Tekan Ctrl+C di jendela ini untuk mematikan server.
echo.

if not exist "seo-boost.exe" (
    echo [ERR] File seo-boost.exe tidak ditemukan!
    echo [INFO] Silakan kompilasi terlebih dahulu dengan: go build -o seo-boost.exe ./cmd/seo-boost
    pause
    exit /b 1
)

.\seo-boost.exe serve --open
pause
