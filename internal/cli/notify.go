package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"seo-boost/internal/config"
	"seo-boost/internal/notify"
	"seo-boost/internal/report"
	"seo-boost/internal/store"
)

func notifyCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)

	always := false
	fs.BoolVar(&always, "always", false, "paksa kirim notifikasi mengabaikan filter perubahan bermakna")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
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

	st := store.NewStore(c.DataDir)
	latestRun, err := st.LoadLatestRun(site.Name)
	if err != nil || latestRun == nil {
		fmt.Fprintf(stderr, "[GAGAL] Belum ada riwayat run untuk situs %s. Jalankan 'seo-boost run' terlebih dahulu.\n", site.Name)
		return 1
	}

	pages, _ := st.LoadPages(site.Name)

	// Cari run sebelumnya untuk menghitung diff
	runFiles, _ := st.ListRuns(site.Name)
	var prevDiff *report.RunDiff
	if len(runFiles) >= 2 {
		// File kedua dari belakang adalah run sebelumnya
		prevFile := runFiles[len(runFiles)-2]
		prevID := strings.TrimSuffix(prevFile, ".json")
		if prevRun, pErr := st.LoadRunByID(site.Name, prevID); pErr == nil {
			prevDiff = report.ComputeDiff(pages, prevRun)
		}
	}
	if prevDiff == nil {
		prevDiff = &report.RunDiff{}
	}

	hasCrit := report.HasCriticalIssues(pages, nil, site.PriorityPaths)
	notifData := &notify.NotificationData{
		Site:              site.Name,
		Timestamp:         latestRun.Timestamp,
		Summary:           latestRun,
		Diff:              prevDiff,
		HasCriticalIssues: hasCrit,
	}

	mode := notify.NotifyAuto
	if always {
		mode = notify.NotifyAlways
	}

	hc := defaultHTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}

	ctx := context.Background()
	sent, nErr := notify.Dispatch(ctx, hc, c.Notify, mode, notifData)
	if errors.Is(nErr, notify.ErrNoChannelsConfigured) {
		fmt.Fprintf(stdout, "[INFO] Tidak ada kanal notifikasi yang aktif (atur env var Telegram atau Discord untuk mengaktifkan).\n")
		return 0
	}
	if nErr != nil {
		fmt.Fprintf(stderr, "[GAGAL] Gagal mengirim notifikasi: %v\n", nErr)
		return 1
	}

	if !sent {
		fmt.Fprintf(stdout, "[INFO] Notifikasi dilewati (tidak ada perubahan bermakna). Gunakan flag --always untuk memaksa kirim.\n")
		return 0
	}

	fmt.Fprintf(stdout, "[OK] Notifikasi berhasil dikirim ke kanal yang aktif.\n")
	return 0
}
