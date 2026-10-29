# seo-boost

> **SEO Automation Tool** mandiri berbasis Go (*standard library first*).
> Menjawab status crawl, indeksasi, performa halaman 1, peluang *keyword gap*, web dashboard lokal, hingga notifikasi otomatis langsung dari **Google Search Console API resmi** tanpa biaya langganan bulanan.

---

## Daftar Isi
- [1. Fitur Utama](#1-fitur-utama)
- [2. Prasyarat Sistem & Kredensial](#2-prasyarat-sistem--kredensial)
- [3. Instalasi & Kompilasi](#3-instalasi--kompilasi)
- [4. Panduan Cepat (Quick Start 5 Menit)](#4-panduan-cepat-quick-start-5-menit)
- [5. Konfigurasi (`config.json`)](#5-konfigurasi-configjson)
- [6. Referensi Perintah CLI](#6-referensi-perintah-cli)
- [7. Web Dashboard Lokal (`serve`)](#7-web-dashboard-lokal-serve)
- [8. Notifikasi Otomatis (`notify`)](#8-notifikasi-otomatis-notify)
- [9. Otomasi Penjadwalan 100% Lokal](#9-otomasi-penjadwalan-100-lokal)
- [10. Quality Gate & Pengujian](#10-quality-gate--pengujian)
- [11. FAQ & Pemecahan Masalah](#11-faq--pemecahan-masalah)

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

## 2. Prasyarat Sistem & Kredensial

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

## 3. Instalasi & Kompilasi

Kompilasi binary mandiri:

```bash
# Windows
go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o seo-boost.exe ./cmd/seo-boost

# Linux / macOS
go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o seo-boost ./cmd/seo-boost
chmod +x seo-boost
```

Verifikasi instalasi:
```bash
./seo-boost version
```

---

## 4. Panduan Cepat (Quick Start 5 Menit)

Berikut alur kerja tercepat dari instalasi hingga melihat laporan:

### Langkah 1: Siapkan Konfigurasi
Salin file konfigurasi contoh:
```bash
cp config.example.json config.json
```
Edit `config.json` dan sesuaikan URL website Anda serta path service account (`secrets/sa.json`).

### Langkah 2: Uji Validitas Konfigurasi & Kredensial
Jalankan diagnosa mandiri:
```bash
./seo-boost doctor --config config.json
```
Jika seluruh indikator menampilkan `[OK]`, koneksi ke Google API dan website target siap digunakan.

### Langkah 3: Jalankan Audit SEO Lengkap
Eksekusi seluruh alur kerja SEO (discover -> crawl -> inspect -> perf -> gap -> report):
```bash
./seo-boost run --site portfolio
```

### Langkah 4: Buka Web Dashboard Lokal
Tampilkan visualisasi audit, daftar halaman, dan peluang keyword langsung di browser:
```bash
./seo-boost serve --open
```

---

## 5. Konfigurasi (`config.json`)

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

## 6. Referensi Perintah CLI

### A. Diagnosa Sistem (`doctor`)
Memvalidasi file konfigurasi, kredensial JSON, jangkauan website, dan hak akses GSC API.
```bash
./seo-boost doctor
./seo-boost doctor --config config.json --site portfolio
```

### B. Menjalankan Pipeline Penuh (`run`)
Mengeksekusi siklus audit lengkap secara berurutan:
```bash
# Menjalankan untuk situs tertentu
./seo-boost run --site portfolio

# Menjalankan untuk semua situs di konfigurasi
./seo-boost run --site all

# Mode simulasi (tanpa memanggil API GSC yang memakan kuota)
./seo-boost run --site portfolio --dry-run
```

### C. Subcommand Modular

Setiap tahapan dapat dipanggil secara terisolasi untuk kebutuhan spesifik:

1. **Penemuan URL (`discover`)**:
   Membaca robots.txt dan sitemap XML rekursif.
   ```bash
   ./seo-boost discover --site portfolio
   ./seo-boost discover --site portfolio --spider
   ```

2. **Crawl & On-Page Audit (`crawl`)**:
   Mengunduh HTML, mengekstrak link internal, dan memeriksa tag SEO on-page.
   ```bash
   ./seo-boost crawl --site portfolio --concurrency 5 --max-pages 100
   ```

3. **Inspeksi Status Indeks GSC (`inspect`)**:
   Memeriksa status halaman langsung ke GSC URL Inspection API.
   ```bash
   ./seo-boost inspect --site portfolio --limit 50
   ./seo-boost inspect --site portfolio --dry-run
   ```

4. **Performa Pencarian GSC (`perf`)**:
   Menarik data kueri, impresi, klik, CTR, posisi ranking, dan menghitung pergeseran periode.
   ```bash
   ./seo-boost perf --site portfolio --days 28
   ```

5. **Keyword Gap Analysis (`gap`)**:
   Membandingkan kueri pencarian dengan tag title, h1, dan isi artikel.
   ```bash
   ./seo-boost gap --site portfolio --min-imp 50
   ```

6. **Ekspor Laporan SEO (`report`)**:
   Menampilkan atau mengekspor hasil audit terakhir ke berbagai format:
   ```bash
   # Terminal text bersih
   ./seo-boost report --site portfolio --format text

   # Ekspor Markdown (tabel GFM)
   ./seo-boost report --site portfolio --format md --out reports/latest.md

   # Ekspor HTML mandiri (tampilan modern dengan CSS inline)
   ./seo-boost report --site portfolio --format html --out reports/latest.html

   # Ekspor JSON (untuk integrasi pipeline data)
   ./seo-boost report --site portfolio --format json --out reports/latest.json

   # Integrasi CI/CD (keluar dengan exit code 2 bila ada isu kritis P1)
   ./seo-boost report --site portfolio --fail-on-issues
   ```

7. **Pengiriman URL & Sitemap (`submit`)**:
   Mengirimkan sitemap ke GSC, atau ping URL baru/berubah ke mesin pencari:
   ```bash
   # Buat kunci verifikasi IndexNow terlebih dahulu
   ./seo-boost indexnow-key

   # Kirim sitemap ke Google Search Console
   ./seo-boost submit --site portfolio --gsc

   # Kirim URL ke IndexNow (Bing, Yandex, Seznam, Naver)
   ./seo-boost submit --site portfolio --indexnow

   # Mode simulasi tanpa mengirim data nyata
   ./seo-boost submit --site portfolio --indexnow --dry-run
   ```

---

## 7. Web Dashboard Lokal (`serve`)

`seo-boost` menyertakan web server lokal bawaan dengan frontend interaktif yang terkompilasi langsung di dalam binary. Tidak memerlukan instalasi Node.js, NPM, atau server web tambahan.

### Menjalankan Server
```bash
# Jalankan pada localhost dengan token acak dan buka browser otomatis
./seo-boost serve --open

# Jalankan pada alamat/port tertentu
./seo-boost serve --addr 127.0.0.1:9090

# Nonaktifkan autentikasi token (hanya diizinkan untuk antarmuka loopback/lokal)
./seo-boost serve --addr 127.0.0.1:8080 --no-auth
```

### Fitur Dashboard Web
- **Panel Ringkasan (Overview)**: Metrik total halaman, skor kesehatan SEO, status indeks Google, ringkasan klik, impresi, dan daftar isu prioritas (`[P1]`).
- **Penjelajah Halaman (Page Explorer)**: Tabel seluruh URL yang terindeks/tidak terindeks, status HTTP, waktu respons, internal link masuk/keluar, dan status kanonikal.
- **Celah Kata Kunci (Keyword Gap)**: Analisis peluang peringkat halaman 2 (posisi 11-20), kata kunci impresi tinggi dengan CTR rendah, serta rekomendasi penempatan kueri.
- **Riwayat Run (Run History)**: Catatan riwayat eksekusi audit lengkap beserta perbandingan perubahan (*diff*).
- **Trigger Run Langsung**: Tombol untuk memicu eksekusi audit situs secara langsung dari browser.

---

## 8. Notifikasi Otomatis (`notify`)

`seo-boost` dapat mengirimkan ringkasan hasil audit langsung ke saluran komunikasi tim Anda tanpa mengekspos token kredensial ke dalam file konfigurasi publik.

### Kredensial via Environment Variable

Untuk menjaga keamanan token bot, gunakan environment variable sistem:

```bash
# Telegram Bot Token
export SEOBOOST_TG_TOKEN="123456789:ABCdefGHIjklMNOpqrSTUvwxyz"

# Discord Webhook URL
export SEOBOOST_DISCORD="https://discord.com/api/webhooks/1234567890/abcdef..."
```

*(Pada Windows PowerShell gunakan `$env:SEOBOOST_TG_TOKEN="..."; $env:SEOBOOST_DISCORD="..."`)*

### Menjalankan Perintah Notifikasi
```bash
# Kirim notifikasi berdasarkan laporan run terakhir
./seo-boost notify --site portfolio

# Paksa kirim notifikasi mengabaikan filter kebisingan (bypass noise reduction)
./seo-boost notify --site portfolio --force

# Kirim berdasarkan file laporan JSON tertentu
./seo-boost notify --site portfolio --report ./reports/audit.json
```

### Filter Peredam Kebisingan (*Noise Reduction*)
- **`auto` (Bawaan)**: Notifikasi hanya dikirim jika terdapat isu baru, isu yang terselesaikan, atau terdapat perubahan performa impresi/klik yang signifikan ($\ge 10\%$). Jika tidak ada perubahan berarti, bot tidak akan mengganggu Anda.
- **`always`**: Selalu mengirim ringkasan setiap kali proses selesai.
- **`off`**: Menonaktifkan pengiriman notifikasi.

---

## 9. Otomasi Penjadwalan 100% Lokal

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

## 10. Quality Gate & Pengujian

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

## 11. FAQ & Pemecahan Masalah

**T: Muncul pesan error `403 Forbidden` atau `User does not have sufficient permission for site on Google Search Console` saat menjalankan `doctor` atau `inspect`.**  
*Jawab:* Pastikan email Service Account (`...@...iam.gserviceaccount.com`) telah ditambahkan sebagai **Owner** atau **Full Permission** pada properti Search Console yang bersangkutan di menu *Settings > Users and permissions*.

**T: Kuota harian URL Inspection habis (`429 Quota Exceeded`).**  
*Jawab:* Google Search Console membatasi panggilan URL Inspection sebanyak 2.000 panggilan per hari per properti. Gunakan flag `--limit` (misal `--limit 100`) atau manfaatkan fitur cache bawaan `reinspect_after_days` pada konfigurasi agar halaman yang baru diperiksa tidak diinspeksi ulang.

**T: Mengapa tidak ada emoji atau ikon visual warna-warni pada terminal?**  
*Jawab:* `seo-boost` mematuhi prinsip keandalan terminal (*pure ASCII badges*). Teks log menggunakan format seragam `[OK]`, `[WARN]`, `[ERR]`, dan `[INFO]` agar bersih, tidak mengalami *character corruption* di berbagai sistem operasi lawas, dan mudah difilter oleh skrip otomatis seperti `grep` atau `awk`.

---

## Lisensi

Didistribusikan di bawah lisensi MIT. Dibuat dengan pendekatan *Ponytail* (sederhana, tangguh, tanpa dependensi pihak ketiga).
