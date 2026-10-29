# seo-boost

> **SEO Automation Tool** mandiri berbasis Go (*standard library first*).
> Menjawab status crawl, indeksasi, performa halaman 1, dan peluang *keyword gap* langsung dari **Google Search Console API resmi** tanpa biaya bulanan.

---

## 1. Fitur Utama

- **Universal & Zero-Framework**: Mendukung website jenis apa pun (Laravel, Next.js, Go, WordPress, HTML statis) hanya bermodalkan URL dan `sitemap.xml`.
- **Standar Library Go & Mandiri**: Tanpa library pihak ketiga yang membengkak, dikompilasi menjadi 1 file binary mandiri (~10 MB).
- **Google Search Console Resmi**:
  - `urlInspection/index:inspect`: Memeriksa status crawl & indeksasi aktual di Google.
  - `searchAnalytics/query`: Menarik data klik, impresi, CTR, dan posisi ranking rata-rata per query.
- **On-Page Audit & Discovery**:
  - Auto-discover sitemap dari `robots.txt` dan parser sitemap XML/gzip rekursif.
  - Crawler internal BFS untuk mendeteksi *orphan page* (halaman tanpa internal link masuk).
  - Audit tag `title`, `meta description`, `h1`, canonical, robots meta `noindex`, gambar tanpa `alt`, dan schema `JSON-LD`.
- **Keyword Gap Analysis**: Membandingkan kata kunci pencarian Google dengan teks halaman untuk menemukan peluang *low-hanging fruit* (`[BOOST]`, `[TITLE_META]`, `[STRONG]`, `[NEW_CONTENT]`).
- **Penyimpanan Atomik & Diff**: Menyimpan riwayat run secara aman (`.tmp` -> `rename`) dan mendeteksi perubahan status indeks atau pergeseran ranking antar run.
- **Output Bersih**: Format teks terminal berbasis badge informatif (`[OK]`, `[WARN]`, `[ERROR]`, `[P1]`, `[+]`, `[-]`, `[UP]`, `[DOWN]`) serta format `json` untuk otomatisasi.

---

## 2. Prasyarat

1. **Go 1.22 atau lebih baru** (jika mengompilasi dari source code).
2. **Google Cloud Service Account**:
   - Aktifkan **Google Search Console API** pada Google Cloud Console.
   - Buat Service Account dan unduh file kunci JSON (simpan di `secrets/sa.json`).
   - Tambahkan email service account (`...iam.gserviceaccount.com`) sebagai *Owner* atau *Full Permission* pada properti Google Search Console Anda.

---

## 3. Instalasi & Build Binary

Kompilasi binary mandiri tanpa dependensi luar:

```bash
# Windows
go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o seo-boost.exe ./cmd/seo-boost

# Linux / macOS
go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o seo-boost ./cmd/seo-boost
```

---

## 4. Konfigurasi (`config.json`)

Salin berkas contoh konfigurasi `config.example.json` menjadi `config.json`:

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
    "stopwords": ["dan", "di", "ke", "dari", "ini", "itu", "untuk"]
  },
  "sites": [
    {
      "name": "portfolio",
      "gsc_property": "sc-domain:example.com",
      "base_url": "https://example.com",
      "sitemaps": ["https://example.com/sitemap.xml"],
      "exclude_paths": ["/admin", "/tag/"],
      "priority_paths": ["/", "/blog/"]
    }
  ]
}
```

---

## 5. Panduan Pemakaian CLI

### Verifikasi Koneksi & Kredensial
Periksa apakah konfigurasi, service account, dan izin properti GSC sudah valid:
```bash
./seo-boost doctor --config config.json
```

### Eksekusi Lengkap (End-to-End MVP)
Jalankan seluruh tahapan analisis SEO (discover → crawl → inspect → perf → gap → report):
```bash
./seo-boost run --site portfolio
```

### Subcommand Modular
Anda juga dapat menjalankan tahapan secara terpisah sesuai kebutuhan:

- **Temukan URL**:
  ```bash
  ./seo-boost discover --site portfolio --spider
  ```
- **Crawl & On-Page Audit**:
  ```bash
  ./seo-boost crawl --site portfolio --concurrency 5 --max-pages 100
  ```
- **Inspeksi Status Indeks GSC**:
  ```bash
  ./seo-boost inspect --site portfolio --limit 50 --dry-run
  ```
- **Tarik Performa Pencarian GSC**:
  ```bash
  ./seo-boost perf --site portfolio --days 28
  ```
- **Analisis Peluang Keyword Gap**:
  ```bash
  ./seo-boost gap --site portfolio --min-imp 50
  ```
- **Cetak Laporan SEO, Diff & Format**:
  ```bash
  # Tampilkan di terminal (teks bersih)
  ./seo-boost report --site portfolio --format text

  # Ekspor format Markdown (GFM tables)
  ./seo-boost report --site portfolio --format md --out report.md

  # Ekspor format HTML mandiri (CSS inline, auto-escaped anti-XSS)
  ./seo-boost report --site portfolio --format html --out report.html

  # Ekspor format JSON untuk mesin / CI
  ./seo-boost report --site portfolio --format json --out report.json

  # Integrasi CI/CD: keluar dengan exit code 2 bila ditemukan masalah kritis
  ./seo-boost report --site portfolio --fail-on-issues
  ```
- **Kirim Sitemap ke GSC & Ping IndexNow**:
  ```bash
  # Buat kunci verifikasi IndexNow
  ./seo-boost indexnow-key

  # Kirim sitemap ke Google Search Console
  ./seo-boost submit --site portfolio --gsc

  # Kirim URL baru/berubah ke IndexNow (Bing, Yandex, Seznam)
  ./seo-boost submit --site portfolio --indexnow

  # Kirim URL ke Google Indexing API (fitur opt-in dua kunci, mode aman)
  ./seo-boost submit --site portfolio --google-indexing

  # Simulasi antrean Google Indexing API mode agresif
  ./seo-boost submit --site portfolio --google-indexing --aggressive --dry-run
  ```

---

## 6. Pengujian & Quality Gate

Proyek ini dilengkapi dengan skrip *quality gate* otomatis yang memvalidasi `vet`, kompilasi, seluruh unit test, dan deteksi data race (`-race`):

```powershell
# Windows PowerShell
.\scripts\gate.ps1 -race
```

---

## 7. Otomasi & Penjadwalan (100% Lokal)

`seo-boost` didesain untuk berjalan otomatis di latar belakang memanfaatkan fasilitas penjadwal bawaan sistem operasi lokal Anda (tanpa perlu menyalakan layanan cloud berbayar atau dependensi pihak ketiga).

### A. Windows Task Scheduler (Setiap Senin 07:00 WIB)
Gunakan skrip pembantu otomatis yang tersedia di `examples/scheduling/setup_task_windows.ps1`:

```powershell
# 1. Simulasi pengecekan perintah (WhatIf)
powershell -ExecutionPolicy Bypass -File .\examples\scheduling\setup_task_windows.ps1 -WhatIf

# 2. Daftarkan tugas terjadwal mingguan secara otomatis
powershell -ExecutionPolicy Bypass -File .\examples\scheduling\setup_task_windows.ps1
```

Tugas akan terdaftar di Windows Task Scheduler dengan nama `SEOBoost_WeeklyAudit` dan menjalankan pipeline penuh setiap Senin pagi, menyimpan laporan terbaru ke `reports/weekly_report.md`.

### B. Linux / macOS Cron Job
Salin baris cron dari `examples/scheduling/crontab.example` ke crontab pengguna (`crontab -e`):

```cron
0 7 * * 1 cd /path/to/seo-boost && ./seo-boost run --site all --format md --out reports/weekly_latest.md >> reports/cron.log 2>&1
```

### C. Pemicu Pasca-Deploy Artikel Baru (Instant Ping)
Segera setelah artikel baru selesai dipublikasikan/dideploy di website Anda, jalankan skrip hook berikut untuk langsung memberi tahu Google dan mesin pencari IndexNow:

```powershell
# Windows
powershell -ExecutionPolicy Bypass -File .\examples\scheduling\post_deploy_hook.ps1 -Site nama_situs

# Linux / macOS
./examples/scheduling/post_deploy_hook.sh nama_situs
```

---

## 8. Lisensi

MIT License. Dibuat dengan pendekatan *Ponytail* (sederhana, efisien, tanpa beban dependensi pihak ketiga).

