# seo-boost

> **SEO Automation Tool** mandiri berbasis Go (*standard library first*).
> Menjawab status crawl, indeksasi, performa halaman 1, peluang *keyword gap*, web dashboard lokal, hingga notifikasi otomatis langsung dari **Google Search Console API resmi** tanpa biaya langganan bulanan.

---

## Daftar Isi
- [1. Fitur Utama](#1-fitur-utama)
- [2. Cara Membuka & Menjalankan Tools](#2-cara-membuka--menjalankan-tools)
- [3. Prasyarat Sistem & Kredensial](#3-prasyarat-sistem--kredensial)
- [4. Instalasi & Kompilasi Binary](#4-instalasi--kompilasi-binary)
- [5. Panduan Cepat (Quick Start 5 Menit)](#5-panduan-cepat-quick-start-5-menit)
- [6. Konfigurasi (`config.json`)](#6-konfigurasi-configjson)
- [7. Referensi Perintah CLI](#7-referensi-perintah-cli)
- [8. Web Dashboard Lokal (`serve`)](#8-web-dashboard-lokal-serve)
- [9. Notifikasi Otomatis (`notify`)](#9-notifikasi-otomatis-notify)
- [10. Otomasi Penjadwalan 100% Lokal](#10-otomasi-penjadwalan-100-lokal)
- [11. Quality Gate & Pengujian](#11-quality-gate--pengujian)
- [12. FAQ & Pemecahan Masalah](#12-faq--pemecahan-masalah)

---

## 1. Fitur Utama

- **Universal & Zero-Framework**: Mendukung website jenis apa pun (Laravel, Next.js, Go, WordPress, HTML statis) hanya bermodalkan URL dan `sitemap.xml`.
- **Standar Library Go & Mandiri**: Tanpa library pihak ketiga yang membengkak, dikompilasi menjadi 1 file binary mandiri (~10 MB) dengan konsumsi memori minimal.
- **Google Search Console Resmi**:
  - `urlInspection/index:inspect`: Memeriksa status crawl, canonical, mobile usability, dan indeksasi aktual di Google.
  - `searchAnalytics/query`: Menarik data klik, impresi, CTR, dan posisi ranking rata-rata per query serta menghitung delta perubahan periode sebelumnya.
- **On-Page Audit & Discovery**:
  - Auto-discover sitemap dari `robots.txt` dan parser sitemap XML/gzip rekursif.
  - Crawler internal BFS untuk mendeteksi *orphan page* (halaman tanpa internal link masuk).
  - Audit tag `title`, `meta description`, `h1`, canonical, robots meta `noindex`, gambar tanpa `alt`, dan schema `JSON-LD`.
- **Keyword Gap Analysis**: Membandingkan kata kunci pencarian Google dengan teks konten halaman untuk menemukan peluang *low-hanging fruit* (`[BOOST]`, `[TITLE_META]`, `[STRONG]`, `[NEW_CONTENT]`).
- **Penyimpanan Atomik & Diff**: Menyimpan riwayat run secara aman (`.tmp` -> `rename`) dan mendeteksi perubahan status indeks atau pergeseran ranking antar run.
- **Web Dashboard Lokal**: Antarmuka web interaktif tanpa dependensi Node.js/eksternal yang tertanam (*embedded*) langsung di dalam binary.
- **Notifikasi Bersih & Cerdas**: Integrasi Telegram dan Discord dengan filter peredam kebisingan (*noise reduction*) otomatis.
- **Output Terminal Bersih**: Menggunakan badge status teks murni (`[OK]`, `[WARN]`, `[ERR]`, `[INFO]`, `[P1]`, `[+]`, `[-]`, `[UP]`, `[DOWN]`) yang ramah terminal dan CI/CD.

---

## 2. Cara Membuka & Menjalankan Tools

> [!IMPORTANT]
> `seo-boost` adalah aplikasi **Command Line Interface (CLI)** dan **Web Dashboard Lokal**.
> **JANGAN klik ganda (double-click) file `seo-boost.exe` langsung dari Windows File Explorer**, karena program CLI akan selesai dalam hitungan detik dan jendela terminal akan langsung tertutup otomatis.

Ada 2 cara praktis untuk membuka dan menggunakan tools ini:

### Cara 1: Membuka Web Dashboard Visual di Browser (Paling Mudah)

Jika Anda ingin melihat tampilan grafik dan antarmuka web di browser:

- **Pengguna Windows (Sekali Klik)**:
  Cukup klik ganda (double-click) file **`start-dashboard.bat`** di folder proyek ini.
  Server web lokal akan aktif dan browser Anda otomatis terbuka menampilkan dashboard visual di alamat `http://127.0.0.1:8080`.

- **Atau Lewat Terminal / PowerShell**:
  ```powershell
  # Windows PowerShell
  .\seo-boost.exe serve --open

  # Linux / macOS
  ./seo-boost serve --open
  ```

---

### Cara 2: Menjalankan Perintah Lewat Terminal / Command Prompt

Untuk menjalankan audit SEO, diagnosa koneksi, atau perintah lainnya:

1. **Buka Terminal di Folder Proyek**:
   - **Di Windows**: Buka folder proyek di File Explorer. Klik pada address bar di bagian atas jendela folder, ketik `powershell` atau `cmd`, lalu tekan **Enter**.
   - **Atau**: Buka aplikasi terminal (PowerShell / Windows Terminal), lalu arahkan ke folder ini:
     ```powershell
     cd "d:\Pembelajaran\coding\SEO Automation Tool"
     ```

2. **Ketik Perintah yang Ingin Dijalankan**:
   - **Cek diagnosa koneksi**:
     ```powershell
     .\seo-boost.exe doctor
     ```
   - **Jalankan audit SEO menyeluruh**:
     ```powershell
     .\seo-boost.exe run --site portfolio
     ```
   - **Melihat panduan bantuan semua perintah**:
     ```powershell
     .\seo-boost.exe --help
     ```

> [!NOTE]
> Format pemanggilan perintah di berbagai sistem operasi:
> - **Windows PowerShell**: Gunakan awalan `.\` contoh: `.\seo-boost.exe doctor`
> - **Windows Command Prompt (CMD)**: Tanpa awalan, contoh: `seo-boost.exe doctor`
> - **Linux / macOS**: Gunakan awalan `./` contoh: `./seo-boost doctor`

---

## 3. Prasyarat Sistem & Kredensial

1. **Go 1.22 atau lebih baru** (hanya diperlukan jika mengompilasi dari source code).
2. **Google Cloud Service Account**:
   - Buka Google Cloud Console dan buat project baru (atau gunakan project yang ada).
   - Aktifkan **Google Search Console API**.
   - Buka menu **IAM & Admin > Service Accounts**, buat Service Account baru.
   - Buat kunci baru bertipe JSON, unduh dan simpan di direktori lokal proyek, misalnya: `secrets/sa.json`.
   - Buka **Google Search Console** properti website Anda.
   - Buka menu **Settings > Users and permissions**, klik **Add User**.
   - Masukkan alamat email service account (`...iam.gserviceaccount.com`) dan berikan hak akses **Owner** atau **Full Permission**.

---

## 4. Instalasi & Kompilasi Binary

Jika Anda perlu mengompilasi ulang binary dari source code:

```bash
# Windows
go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o seo-boost.exe ./cmd/seo-boost

# Linux / macOS
go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o seo-boost ./cmd/seo-boost
chmod +x seo-boost
```

Verifikasi instalasi:
```powershell
.\seo-boost.exe version
```

---

## 5. Panduan Cepat (Quick Start 5 Menit)

Berikut alur kerja tercepat dari instalasi hingga melihat laporan:

### Langkah 1: Siapkan Konfigurasi
Salin file konfigurasi contoh:
```bash
cp config.example.json config.json
```
Edit `config.json` dan sesuaikan URL website Anda serta path service account (`secrets/sa.json`).

### Langkah 2: Uji Validitas Konfigurasi & Kredensial
Jalankan diagnosa mandiri:
```powershell
.\seo-boost.exe doctor --config config.json
```
Jika seluruh indikator menampilkan `[OK]`, koneksi ke Google API dan website target siap digunakan.

### Langkah 3: Jalankan Audit SEO Lengkap
Eksekusi seluruh alur kerja SEO (discover -> crawl -> inspect -> perf -> gap -> report):
```powershell
.\seo-boost.exe run --site portfolio
```

### Langkah 4: Buka Web Dashboard Lokal
Tampilkan visualisasi audit, daftar halaman, dan peluang keyword langsung di browser:
```powershell
.\seo-boost.exe serve --open
```

---

## 6. Konfigurasi (`config.json`)

Berikut adalah struktur lengkap file konfigurasi `config.json`:

```json
{
  "service_account_file": "./secrets/sa.json",
  "data_dir": "./data",
  "defaults": {
    "user_agent": "seo-boost/1.0 (+https://example.com)",
    "crawl_concurrency": 5,
    "crawl_delay_ms": 200,
    "crawl_timeout_s": 20,
    "max_pages": 5000,
    "inspect_rpm": 300,
    "inspect_daily_budget": 1800,
    "reinspect_after_days": 7,
    "perf_days": 28,
    "stopwords": ["dan", "di", "ke", "dari", "ini", "itu", "untuk", "the", "a", "an"]
  },
  "sites": [
    {
      "name": "portfolio",
      "gsc_property": "sc-domain:example.com",
      "base_url": "https://example.com",
      "sitemaps": [
        "https://example.com/sitemap.xml"
      ],
      "exclude_paths": [
        "/admin",
        "/tag/",
        "/author/"
      ],
      "priority_paths": [
        "/",
        "/blog/",
        "/products/"
      ],
      "telegram_chat_id": "123456789",
      "noise_reduction": "auto"
    }
  ]
}
```

### Penjelasan Parameter Utama

| Bidang | Penjelasan |
| --- | --- |
| `service_account_file` | Path relatif/absolut menuju file kredensial Google Service Account JSON |
| `data_dir` | Direktori penyimpanan riwayat snapshot run dan diff laporan |
| `gsc_property` | Target properti GSC (`sc-domain:domain.com` untuk Domain Property atau `https://domain.com/` untuk URL-prefix) |
| `base_url` | Root URL website target |
| `sitemaps` | Daftar sitemap XML utama (opsional, crawler juga auto-discover via robots.txt) |
| `exclude_paths` | Awalan path URL yang diabaikan saat crawl |
| `priority_paths` | Path yang diprioritaskan saat kuota inspect GSC terbatas |
| `telegram_chat_id` | ID Chat/Grup Telegram untuk pengiriman notifikasi |
| `noise_reduction` | Mode filter notifikasi: `auto` (hanya kirim bila ada delta/isu penting), `always`, atau `off` |

---

## 7. Referensi Perintah CLI

### A. Diagnosa Sistem (`doctor`)
Memvalidasi file konfigurasi, kredensial JSON, jangkauan website, dan hak akses GSC API.
```powershell
.\seo-boost.exe doctor
.\seo-boost.exe doctor --config config.json --site portfolio
```

### B. Menjalankan Pipeline Penuh (`run`)
Mengeksekusi siklus audit lengkap secara berurutan:
```powershell
# Menjalankan untuk situs tertentu
.\seo-boost.exe run --site portfolio

# Menjalankan untuk semua situs di konfigurasi
.\seo-boost.exe run --site all

# Mode simulasi (tanpa memanggil API GSC yang memakan kuota)
.\seo-boost.exe run --site portfolio --dry-run
```

### C. Subcommand Modular

Setiap tahapan dapat dipanggil secara terisolasi untuk kebutuhan spesifik:

1. **Penemuan URL (`discover`)**:
   Membaca robots.txt dan sitemap XML rekursif.
   ```powershell
   .\seo-boost.exe discover --site portfolio
   .\seo-boost.exe discover --site portfolio --spider
   ```

2. **Crawl & On-Page Audit (`crawl`)**:
   Mengunduh HTML, mengekstrak link internal, dan memeriksa tag SEO on-page.
   ```powershell
   .\seo-boost.exe crawl --site portfolio --concurrency 5 --max-pages 100
   ```

3. **Inspeksi Status Indeks GSC (`inspect`)**:
   Memeriksa status halaman langsung ke GSC URL Inspection API.
   ```powershell
   .\seo-boost.exe inspect --site portfolio --limit 50
   .\seo-boost.exe inspect --site portfolio --dry-run
   ```

4. **Performa Pencarian GSC (`perf`)**:
   Menarik data kueri, impresi, klik, CTR, posisi ranking, dan menghitung pergeseran periode.
   ```powershell
   .\seo-boost.exe perf --site portfolio --days 28
   ```

5. **Keyword Gap Analysis (`gap`)**:
   Membandingkan kueri pencarian dengan tag title, h1, dan isi artikel.
   ```powershell
   .\seo-boost.exe gap --site portfolio --min-imp 50
   ```

6. **Ekspor Laporan SEO (`report`)**:
   Menampilkan atau mengekspor hasil audit terakhir ke berbagai format:
   ```powershell
   # Terminal text bersih
   .\seo-boost.exe report --site portfolio --format text

   # Ekspor Markdown (tabel GFM)
   .\seo-boost.exe report --site portfolio --format md --out reports/latest.md

   # Ekspor HTML mandiri (tampilan modern dengan CSS inline)
   .\seo-boost.exe report --site portfolio --format html --out reports/latest.html

   # Ekspor JSON (untuk integrasi pipeline data)
   .\seo-boost.exe report --site portfolio --format json --out reports/latest.json

   # Integrasi CI/CD (keluar dengan exit code 2 bila ada isu kritis P1)
   .\seo-boost.exe report --site portfolio --fail-on-issues
   ```

7. **Pengiriman URL & Sitemap (`submit`)**:
   Mengirimkan sitemap ke GSC, atau ping URL baru/berubah ke mesin pencari:
   ```powershell
   # Buat kunci verifikasi IndexNow terlebih dahulu
   .\seo-boost.exe indexnow-key

   # Kirim sitemap ke Google Search Console
   .\seo-boost.exe submit --site portfolio --gsc

   # Kirim URL ke IndexNow (Bing, Yandex, Seznam, Naver)
   .\seo-boost.exe submit --site portfolio --indexnow

   # Mode simulasi tanpa mengirim data nyata
   .\seo-boost.exe submit --site portfolio --indexnow --dry-run
   ```

---

## 8. Web Dashboard Lokal (`serve`)

`seo-boost` menyertakan web server lokal bawaan dengan frontend interaktif yang terkompilasi langsung di dalam binary. Tidak memerlukan instalasi Node.js, NPM, atau server web tambahan.

### Menjalankan Server
```powershell
# Jalankan pada localhost dengan token acak dan buka browser otomatis
.\seo-boost.exe serve --open

# Jalankan pada alamat/port tertentu
.\seo-boost.exe serve --addr 127.0.0.1:9090

# Nonaktifkan autentikasi token (hanya diizinkan untuk antarmuka loopback/lokal)
.\seo-boost.exe serve --addr 127.0.0.1:8080 --no-auth
```

### Fitur Dashboard Web
- **Panel Ringkasan (Overview)**: Metrik total halaman, skor kesehatan SEO, status indeks Google, ringkasan klik, impresi, dan daftar isu prioritas (`[P1]`).
- **Penjelajah Halaman (Page Explorer)**: Tabel seluruh URL yang terindeks/tidak terindeks, status HTTP, waktu respons, internal link masuk/keluar, dan status kanonikal.
- **Celah Kata Kunci (Keyword Gap)**: Analisis peluang peringkat halaman 2 (posisi 11-20), kata kunci impresi tinggi dengan CTR rendah, serta rekomendasi penempatan kueri.
- **Riwayat Run (Run History)**: Catatan riwayat eksekusi audit lengkap beserta perbandingan perubahan (*diff*).
- **Trigger Run Langsung**: Tombol untuk memicu eksekusi audit situs secara langsung dari browser.

---

## 9. Notifikasi Otomatis (`notify`)

`seo-boost` dapat mengirimkan ringkasan hasil audit langsung ke saluran komunikasi tim Anda tanpa mengekspos token kredensial ke dalam file konfigurasi publik.

### Kredensial via Environment Variable

Untuk menjaga keamanan token bot, gunakan environment variable sistem:

```powershell
# Windows PowerShell
$env:SEOBOOST_TG_TOKEN="123456789:ABCdefGHIjklMNOpqrSTUvwxyz"
$env:SEOBOOST_DISCORD="https://discord.com/api/webhooks/1234567890/abcdef..."

# Linux / macOS Bash
export SEOBOOST_TG_TOKEN="123456789:ABCdefGHIjklMNOpqrSTUvwxyz"
export SEOBOOST_DISCORD="https://discord.com/api/webhooks/1234567890/abcdef..."
```

### Menjalankan Perintah Notifikasi
```powershell
# Kirim notifikasi berdasarkan laporan run terakhir
.\seo-boost.exe notify --site portfolio

# Paksa kirim notifikasi mengabaikan filter kebisingan (bypass noise reduction)
.\seo-boost.exe notify --site portfolio --force

# Kirim berdasarkan file laporan JSON tertentu
.\seo-boost.exe notify --site portfolio --report ./reports/audit.json
```

### Filter Peredam Kebisingan (*Noise Reduction*)
- **`auto` (Bawaan)**: Notifikasi hanya dikirim jika terdapat isu baru, isu yang terselesaikan, atau terdapat perubahan performa impresi/klik yang signifikan (>= 10%). Jika tidak ada perubahan berarti, bot tidak akan mengganggu Anda.
- **`always`**: Selalu mengirim ringkasan setiap kali proses selesai.
- **`off`**: Menonaktifkan pengiriman notifikasi.

---

## 10. Otomasi Penjadwalan 100% Lokal

Tidak perlu menyewa server cloud atau layanan CI/CD berbayar. Anda dapat menjadwalkan audit berjalan otomatis di komputer atau server lokal menggunakan fitur bawaan sistem operasi.

### A. Windows Task Scheduler (Audit Mingguan)
Gunakan skrip PowerShell pembantu yang telah disediakan:

```powershell
# 1. Uji coba simulasi tanpa mengubah sistem (-WhatIf)
powershell -ExecutionPolicy Bypass -File .\examples\scheduling\setup_task_windows.ps1 -WhatIf

# 2. Daftarkan tugas terjadwal otomatis (Setiap Senin 07:00 WIB)
powershell -ExecutionPolicy Bypass -File .\examples\scheduling\setup_task_windows.ps1
```

Tugas akan otomatis terdaftar di Windows Task Scheduler dengan nama `SEOBoost_WeeklyAudit`.

### B. Linux / macOS Cron Job
Buka crontab pengguna (`crontab -e`) dan tambahkan baris jadwal:

```cron
# Jalankan audit berkala setiap hari Senin pukul 07:00 pagi
0 7 * * 1 cd /path/to/seo-boost && ./seo-boost run --site portfolio --format md --out reports/weekly_latest.md >> reports/cron.log 2>&1
```

### C. Hook Pasca-Deploy Artikel Baru
Segera setelah artikel baru selesai dipublikasikan di blog atau CMS, jalankan skrip hook untuk langsung mengirimkan URL ke Google Search Console dan IndexNow:

```powershell
# Windows
powershell -ExecutionPolicy Bypass -File .\examples\scheduling\post_deploy_hook.ps1 -Site portfolio

# Linux / macOS
./examples/scheduling/post_deploy_hook.sh portfolio
```

---

## 11. Quality Gate & Pengujian

Sebelum merilis atau mengubah kode, jalankan pengujian kepatuhan kualitas menyeluruh (*vetting*, tes unit, dan deteksi data race):

```powershell
# Windows PowerShell
.\scripts\gate.ps1 -race

# Linux / macOS Bash
./scripts/gate.sh -race
```

Semua pengujian dirancang untuk memenuhi standar ketat:
- `go vet` bersih 100% tanpa peringatan.
- `go test -race` lulus tanpa adanya kondisi data race.
- Penanganan error eksplisit pada setiap alur I/O.

---

## 12. FAQ & Pemecahan Masalah

**T: Kenapa file `seo-boost.exe` saat di-double click langsung menutup sendiri?**  
*Jawab:* Karena `seo-boost` adalah program Command Line (CLI). Program CLI dijalankan melalui Terminal (PowerShell atau Command Prompt) atau dibuka sebagai Web Dashboard. Jika Anda ingin membuka tampilan visual dengan sekali klik di Windows, klik dua kali file **`start-dashboard.bat`**.

**T: Muncul pesan error `403 Forbidden` atau `User does not have sufficient permission for site on Google Search Console` saat menjalankan `doctor` atau `inspect`.**  
*Jawab:* Pastikan email Service Account (`...@...iam.gserviceaccount.com`) telah ditambahkan sebagai **Owner** atau **Full Permission** pada properti Search Console yang bersangkutan di menu *Settings > Users and permissions*.

**T: Kuota harian URL Inspection habis (`429 Quota Exceeded`).**  
*Jawab:* Google Search Console membatasi panggilan URL Inspection sebanyak 2.000 panggilan per hari per properti. Gunakan flag `--limit` (misal `--limit 100`) atau manfaatkan fitur cache bawaan `reinspect_after_days` pada konfigurasi agar halaman yang baru diperiksa tidak diinspeksi ulang.

**T: Mengapa tidak ada emoji atau ikon visual warna-warni pada terminal?**  
*Jawab:* `seo-boost` mematuhi prinsip keandalan terminal (*pure ASCII badges*). Teks log menggunakan format seragam `[OK]`, `[WARN]`, `[ERR]`, dan `[INFO]` agar bersih, tidak mengalami *character corruption* di berbagai sistem operasi lawas, dan mudah difilter oleh skrip otomatis seperti `grep` atau `awk`.

---

## Lisensi

Didistribusikan di bawah lisensi MIT. Dibuat dengan pendekatan *Ponytail* (sederhana, tangguh, tanpa dependensi pihak ketiga).
