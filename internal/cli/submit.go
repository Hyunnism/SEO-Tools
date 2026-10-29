package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"seo-boost/internal/config"
	"seo-boost/internal/google"
	"seo-boost/internal/gsc"
	"seo-boost/internal/indexing"
	"seo-boost/internal/indexnow"
	"seo-boost/internal/model"
	"seo-boost/internal/sitemap"
	"seo-boost/internal/store"
)

func indexnowKeyCmd(args []string, stdout, stderr io.Writer) int {
	key, err := indexnow.GenerateKey()
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Membuat kunci IndexNow: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "================================================================================\n")
	fmt.Fprintf(stdout, "  INDEXNOW KEY GENERATOR\n")
	fmt.Fprintf(stdout, "================================================================================\n\n")
	fmt.Fprintf(stdout, "Kunci baru berhasil dibuat:\n")
	fmt.Fprintf(stdout, "  %s\n\n", key)
	fmt.Fprintf(stdout, "Langkah integrasi verifikasi kepemilikan:\n")
	fmt.Fprintf(stdout, "1. Buat berkas teks di direktori root website Anda:\n")
	fmt.Fprintf(stdout, "   Nama Berkas : %s.txt\n", key)
	fmt.Fprintf(stdout, "   Isi Berkas  : %s\n\n", key)
	fmt.Fprintf(stdout, "2. Tambahkan kunci ini ke file config.json di bawah properti situs:\n")
	fmt.Fprintf(stdout, "   \"indexnow_key\": %q\n\n", key)
	fmt.Fprintf(stdout, "3. Uji dan kirim URL dengan perintah:\n")
	fmt.Fprintf(stdout, "   seo-boost submit --site <nama_site> --indexnow\n")

	return 0
}

func submitCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("submit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)

	var flagGSC bool
	var flagIndexNow bool
	var flagGoogleIndexing bool
	var flagAggressive bool
	var flagDryRun bool
	var flagAll bool
	var flagKey string
	var flagDailyCap int

	fs.BoolVar(&flagGSC, "gsc", false, "submit sitemap ke Google Search Console")
	fs.BoolVar(&flagIndexNow, "indexnow", false, "ping URL baru/berubah ke IndexNow")
	fs.BoolVar(&flagGoogleIndexing, "google-indexing", false, "submit URL ke Google Indexing API (opt-in dua kunci)")
	fs.BoolVar(&flagAggressive, "aggressive", false, "aktifkan mode agresif Google Indexing API")
	fs.BoolVar(&flagDryRun, "dry-run", false, "simulasikan pengiriman tanpa memanggil API eksternal")
	fs.BoolVar(&flagAll, "all", false, "kirim seluruh URL valid (bukan hanya yang baru/berubah)")
	fs.StringVar(&flagKey, "key", "", "override kunci IndexNow dari config")
	fs.IntVar(&flagDailyCap, "daily-cap", 0, "override daily_cap untuk Google Indexing API")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	// Jika pengguna tidak menentukan target, default ke --gsc
	if !flagGSC && !flagIndexNow && !flagGoogleIndexing {
		flagGSC = true
	}

	c, err := config.Load(g.config)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Muat config: %v\n", err)
		return 1
	}

	site, err := resolveSite(c, g.site)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] %v\n", err)
		return 1
	}

	log := newLogger(stderr, g.verbose)
	ctx := context.Background()

	hc := defaultHTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	fmt.Fprintf(stdout, "================================================================================\n")
	fmt.Fprintf(stdout, "  SEO-BOOST SUBMIT: %s (%s)\n", site.Name, site.BaseURL)
	fmt.Fprintf(stdout, "================================================================================\n\n")

	// -------------------------------------------------------------------------
	// 1. SUBMIT GSC SITEMAP
	// -------------------------------------------------------------------------
	if flagGSC {
		fmt.Fprintf(stdout, "--- [GSC] PENGIRIMAN SITEMAP KE GOOGLE SEARCH CONSOLE ---\n")
		var sitemapsToSend []string
		if len(site.Sitemaps) > 0 {
			sitemapsToSend = append(sitemapsToSend, site.Sitemaps...)
		} else {
			discovered, dErr := sitemap.AutoDiscover(ctx, hc, site.BaseURL)
			if dErr == nil && len(discovered) > 0 {
				sitemapsToSend = append(sitemapsToSend, discovered...)
			}
		}

		if len(sitemapsToSend) == 0 {
			fmt.Fprintf(stderr, "  [WARN] Tidak ada sitemap yang dikonfigurasi atau ditemukan untuk %s\n\n", site.Name)
		} else {
			var gOpts []google.ClientOption
			gOpts = append(gOpts, google.WithLogger(log))
			if defaultHTTPClient != nil {
				gOpts = append(gOpts, google.WithHTTPClient(defaultHTTPClient))
			}

			gClient, gErr := google.NewClientFromFile(c.ServiceAccountFile, gOpts...)
			if gErr != nil {
				fmt.Fprintf(stderr, "  [ERROR] Inisialisasi Google client gagal: %v\n\n", gErr)
			} else {
				for _, sm := range sitemapsToSend {
					if flagDryRun {
						fmt.Fprintf(stdout, "  [DRY RUN] [GSC] Akan mengirim sinyal sitemap: %s\n", sm)
					} else {
						subErr := gsc.SubmitSitemap(ctx, gClient, site.GSCProperty, sm)
						if subErr != nil {
							fmt.Fprintf(stderr, "  [ERROR] GSC submit gagal untuk %s: %v\n", sm, subErr)
						} else {
							fmt.Fprintf(stdout, "  [OK] [GSC] Berhasil mengirim sinyal sitemap: %s\n", sm)
						}
					}
				}

				// Tampilkan status sitemap dari GSC
				if !flagDryRun {
					smList, lErr := gsc.ListSitemaps(ctx, gClient, site.GSCProperty)
					if lErr != nil {
						log.Warn("gagal mengambil status sitemap GSC", "error", lErr)
					} else if len(smList) > 0 {
						fmt.Fprintf(stdout, "\n  Status Sitemap di Google Search Console:\n")
						tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
						fmt.Fprintf(tw, "  Path\tStatus\tDiunduh Terakhir\tErrors\tWarnings\tSubmitted/Indexed\n")
						for _, item := range smList {
							st := "[PROCESSED]"
							if item.IsPending {
								st = "[PENDING]"
							}
							lastDl := "-"
							if item.LastDownloaded != nil {
								lastDl = item.LastDownloaded.Format("2006-01-02 15:04")
							}
							subIdx := "-"
							if len(item.Contents) > 0 {
								subIdx = fmt.Sprintf("%s / %s", item.Contents[0].Submitted, item.Contents[0].Indexed)
							}
							fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\n",
								item.Path, st, lastDl, item.Errors, item.Warnings, subIdx)
						}
						tw.Flush()
					}
				}
				fmt.Fprintln(stdout)
			}
		}
	}

	// -------------------------------------------------------------------------
	// 2. SUBMIT INDEXNOW (Bing, Yandex, Seznam, Naver)
	// -------------------------------------------------------------------------
	if flagIndexNow {
		fmt.Fprintf(stdout, "--- [INDEXNOW] PENGIRIMAN URL KE INDEXNOW ---\n")
		key := flagKey
		if key == "" {
			key = site.IndexNowKey
		}

		if key == "" {
			fmt.Fprintf(stderr, "  [ERROR] Kunci IndexNow belum diatur untuk situs %s.\n", site.Name)
			fmt.Fprintf(stderr, "          Gunakan 'seo-boost indexnow-key' untuk membuat kunci baru.\n\n")
			return 1
		}

		u, parseErr := url.Parse(site.BaseURL)
		if parseErr != nil || u.Host == "" {
			fmt.Fprintf(stderr, "  [ERROR] BaseURL situs %q tidak valid: %v\n\n", site.BaseURL, parseErr)
			return 1
		}
		host := u.Host
		keyLocation := fmt.Sprintf("%s/%s.txt", strings.TrimRight(site.BaseURL, "/"), key)

		// 1. Verifikasi kepemilikan file kunci di server
		if !flagDryRun {
			fmt.Fprintf(stdout, "  Memverifikasi file kunci di %s...\n", keyLocation)
			if vErr := indexnow.VerifyKeyFile(ctx, hc, keyLocation, key); vErr != nil {
				fmt.Fprintf(stderr, "  [ERROR] Verifikasi file kunci IndexNow gagal: %v\n", vErr)
				fmt.Fprintf(stderr, "          Pengiriman IndexNow dibatalkan demi keamanan.\n\n")
				return 1
			}
			fmt.Fprintf(stdout, "  [OK] File kunci IndexNow terverifikasi valid.\n")
		} else {
			fmt.Fprintf(stdout, "  [DRY RUN] Melewati verifikasi file kunci (%s)\n", keyLocation)
		}

		// 2. Tentukan daftar URL yang akan dikirim
		st := store.NewStore(c.DataDir)
		pages, _ := st.LoadPages(site.Name)
		prevRun, _ := st.LoadLatestRun(site.Name)

		var urlsToSend []string
		if flagAll || prevRun == nil || prevRun.PageSummaries == nil {
			// Kirim seluruh URL valid jika --all atau belum ada riwayat run sebelumnya
			for _, p := range pages {
				if isValidIndexNowCandidate(p) {
					urlsToSend = append(urlsToSend, p.URL)
				}
			}
			if len(urlsToSend) == 0 && len(pages) == 0 {
				urlsToSend = append(urlsToSend, site.BaseURL)
			}
		} else {
			// Saring hanya URL baru atau berubah status/peringkat sejak run terakhir
			for _, p := range pages {
				if !isValidIndexNowCandidate(p) {
					continue
				}
				oldStub, exists := prevRun.PageSummaries[p.URL]
				if !exists {
					// URL baru ditemukan
					urlsToSend = append(urlsToSend, p.URL)
				} else if p.Index != nil && p.Index.Status != oldStub.Status {
					// Status indeksasi berubah
					urlsToSend = append(urlsToSend, p.URL)
				}
			}
		}

		if len(urlsToSend) == 0 {
			fmt.Fprintf(stdout, "  [INFO] Tidak ada URL baru atau berubah yang perlu dikirim ke IndexNow.\n\n")
			return 0
		}

		fmt.Fprintf(stdout, "  Jumlah URL kandidat kirim: %d URL\n", len(urlsToSend))

		if flagDryRun {
			fmt.Fprintf(stdout, "  [DRY RUN] [INDEXNOW] Simulasi pengiriman %d URL ke https://api.indexnow.org/indexnow\n", len(urlsToSend))
			maxShow := 10
			if len(urlsToSend) < maxShow {
				maxShow = len(urlsToSend)
			}
			for i := 0; i < maxShow; i++ {
				fmt.Fprintf(stdout, "    - %s\n", urlsToSend[i])
			}
			if len(urlsToSend) > maxShow {
				fmt.Fprintf(stdout, "    ... dan %d URL lainnya\n", len(urlsToSend)-maxShow)
			}
			fmt.Fprintln(stdout)
			return 0
		}

		// 3. Kirim ke IndexNow
		submitter := indexnow.NewSubmitter("", hc)
		res, subErr := submitter.Submit(ctx, host, key, keyLocation, urlsToSend)
		if subErr != nil {
			fmt.Fprintf(stderr, "  [ERROR] Pengiriman IndexNow gagal: %v\n\n", subErr)
			return 1
		}

		fmt.Fprintf(stdout, "  [OK] [INDEXNOW] %s (Total: %d URL, Batches: %d, HTTP: %d)\n\n",
			res.Message, res.TotalSubmitted, res.Batches, res.StatusCode)
	}

	// -------------------------------------------------------------------------
	// 3. SUBMIT GOOGLE INDEXING API (Opt-in Dua Kunci)
	// -------------------------------------------------------------------------
	if flagGoogleIndexing {
		fmt.Fprintf(stdout, "--- [GOOGLE INDEXING API] PENGIRIMAN NOTIFIKASI INDEXING ---\n")

		idxCfg := c.EffectiveGoogleIndexing(site)
		if flagAggressive {
			idxCfg.Mode = "aggressive"
		}
		if flagDailyCap > 0 {
			idxCfg.DailyCap = flagDailyCap
		}
		isAggressive := idxCfg.Mode == "aggressive"

		// Validasi Dua Kunci (Two-Key Activation)
		if !isAggressive && !idxCfg.Enabled {
			fmt.Fprintf(stderr, "  [ERROR] Google Indexing API belum diaktifkan di konfigurasi (kunci 1 non-aktif).\n")
			fmt.Fprintf(stderr, "          Fitur ini menerapkan aktivasi dua kunci (two-key activation) untuk mencegah pemanggilan tanpa sengaja.\n")
			fmt.Fprintf(stderr, "          Tambahkan di config.json: \"google_indexing\": {\"enabled\": true}\n")
			fmt.Fprintf(stderr, "          Atau gunakan mode agresif: seo-boost submit --site %s --google-indexing --aggressive\n\n", site.Name)
			return 1
		}

		// Cetak peringatan kebijakan resmi Google tepat satu kali
		fmt.Fprintln(stdout, indexing.PolicyWarning)
		fmt.Fprintln(stdout)

		st := store.NewStore(c.DataDir)
		pages, _ := st.LoadPages(site.Name)
		idxStore, err := indexing.LoadIndexingStore(c.DataDir, site.Name)
		if err != nil {
			log.Warn("gagal memuat indexing.json, membuat baru", "error", err)
			idxStore = &indexing.SiteIndexingStore{Records: make(map[string]indexing.IndexingRecord)}
		}

		queue := indexing.BuildQueue(pages, idxStore, idxCfg, site.PriorityPaths, isAggressive, time.Now().UTC())

		if len(queue) == 0 {
			fmt.Fprintf(stdout, "  [INFO] Tidak ada URL yang memenuhi syarat pengiriman ke Google Indexing API saat ini.\n\n")
		} else if flagDryRun {
			fmt.Fprintf(stdout, "  [DRY RUN] [INDEXING] %d URL dijadwalkan untuk dikirim (Mode: %s, Daily Cap: %d):\n",
				len(queue), idxCfg.Mode, idxCfg.DailyCap)
			for i, item := range queue {
				fmt.Fprintf(stdout, "    %2d. [P%d: %s] %s (%s)\n", i+1, item.Priority, item.Reason, item.URL, item.Type)
			}
			fmt.Fprintln(stdout)
		} else {
			var gOpts []google.ClientOption
			gOpts = append(gOpts, google.WithLogger(log))
			if defaultHTTPClient != nil {
				gOpts = append(gOpts, google.WithHTTPClient(defaultHTTPClient))
			}

			gClient, gErr := google.NewClientFromFile(c.ServiceAccountFile, gOpts...)
			if gErr != nil {
				fmt.Fprintf(stderr, "  [ERROR] Inisialisasi Google client gagal: %v\n\n", gErr)
				return 1
			}

			fmt.Fprintf(stdout, "  Mengirim %d URL ke Google Indexing API (Mode: %s)...\n", len(queue), idxCfg.Mode)
			successCount := 0
			failCount := 0
			stopImmediate := false

			for i, item := range queue {
				if stopImmediate {
					break
				}
				// Jeda kecil antar request (maks 180 req/menit)
				if i > 0 {
					time.Sleep(350 * time.Millisecond)
				}

				_, pubErr := indexing.PublishURL(ctx, gClient, item.URL, item.Type)
				nowSent := time.Now().UTC()
				if pubErr != nil {
					failCount++
					fmt.Fprintf(stderr, "    [-] [GAGAL] %s (%s): %v\n", item.URL, item.Type, pubErr)

					// Fail-Fast: Henti total jika 403 atau 429
					errStr := pubErr.Error()
					if strings.Contains(errStr, "HTTP 403") {
						fmt.Fprintf(stderr, "\n  [ERROR] [403 FORBIDDEN] Akun Service Account bukan Owner properti di GSC atau Web Search Indexing API belum diaktifkan di Google Cloud Console. Pengiriman dihentikan total.\n\n")
						stopImmediate = true
					} else if strings.Contains(errStr, "HTTP 429") {
						fmt.Fprintf(stderr, "\n  [ERROR] [429 RATE LIMIT] Kuota harian/menit Google Indexing API terlampaui. Pengiriman dihentikan total.\n\n")
						stopImmediate = true
					}
				} else {
					successCount++
					fmt.Fprintf(stdout, "    [+] [%s] %s\n", item.Type, item.URL)

					// Catat riwayat pengiriman
					prevRec := idxStore.Records[item.URL]
					sendCount := prevRec.SendCount + 1
					if prevRec.ContentHash != item.ContentHash {
						sendCount = 1
					}
					idxStore.Records[item.URL] = indexing.IndexingRecord{
						URL:         item.URL,
						Type:        item.Type,
						ContentHash: item.ContentHash,
						SentAt:      nowSent,
						HTTPStatus:  http.StatusOK,
						SendCount:   sendCount,
					}
				}
			}

			// Simpan riwayat pengiriman secara atomik
			_ = indexing.SaveIndexingStore(c.DataDir, site.Name, idxStore)

			// Tampilkan laporan terukur
			fmt.Fprintf(stdout, "\n  Ringkasan Pengiriman: %d Terkirim [OK], %d Gagal [WARN]\n", successCount, failCount)

			// Hitung latensi crawl rata-rata
			avgLatency, samples := indexing.CalculateAverageCrawlLatency(pages, idxStore)
			if samples > 0 {
				fmt.Fprintf(stdout, "  Hasil Terukur: Rata-rata selang kirim -> crawl Googlebot: %v (%d sampel)\n", avgLatency.Round(time.Minute), samples)
			}
			fmt.Fprintln(stdout)
		}
	}

	return 0
}

func isValidIndexNowCandidate(p *model.Page) bool {
	if p == nil || p.URL == "" {
		return false
	}
	// Hindari URL error atau yang mengandung noindex
	if p.Crawl != nil {
		if p.Crawl.HTTPStatus != 0 && p.Crawl.HTTPStatus != http.StatusOK {
			return false
		}
		if strings.Contains(strings.ToLower(p.Crawl.RobotsMeta), "noindex") {
			return false
		}
	}
	if p.Index != nil && p.Index.Status == model.StatusBlocked {
		return false
	}
	return true
}

