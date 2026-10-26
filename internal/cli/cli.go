package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"

	"seo-boost/internal/config"
	"seo-boost/internal/google"
)

const usage = `seo-boost - SEO automation tool

Pemakaian: seo-boost <perintah> [flag]

Perintah tersedia:
  version   tampilkan versi
  doctor    periksa konfigurasi, kredensial, dan izin properti GSC
  help      tampilkan bantuan ini

Flag global: --config --site --format --out --verbose
`

type global struct {
	config, site, format, out string
	verbose                   bool
}

func globalFlags(fs *flag.FlagSet) *global {
	g := &global{}
	fs.StringVar(&g.config, "config", "./config.json", "path ke config.json")
	fs.StringVar(&g.site, "site", "", `nama site di config ("all" = semua)`)
	fs.StringVar(&g.format, "format", "text", "text | json | md | html")
	fs.StringVar(&g.out, "out", "", "tulis laporan ke file")
	fs.BoolVar(&g.verbose, "verbose", false, "log detail")
	return g
}

func newLogger(w io.Writer, verbose bool) *slog.Logger {
	lvl := slog.LevelInfo
	if verbose {
		lvl = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl}))
}

// defaultHTTPClient dapat dioverride di test
var defaultHTTPClient *http.Client

// Run mengembalikan exit code: 0 sukses, 1 error runtime/penggunaan.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 1
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "doctor":
		return doctor(rest, stdout, stderr)
	case "discover", "crawl", "inspect", "perf", "gap", "submit", "run", "report", "serve", "indexnow-key":
		fmt.Fprintf(stderr, "perintah %q belum tersedia\n", cmd)
		return 1
	default:
		fmt.Fprintf(stderr, "perintah tidak dikenal: %q\n\n%s", cmd, usage)
		return 1
	}
}

func doctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)
	configOnly := false
	fs.BoolVar(&configOnly, "config-only", false, "hanya validasi config tanpa menghubungi Google API")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	log := newLogger(stderr, g.verbose)

	// 1. Periksa versi Go
	goVer := runtime.Version()
	log.Debug("memeriksa versi Go runtime", "version", goVer)
	if err := checkGoVersion(goVer); err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Versi Go: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "[OK] Versi Go: %s (memenuhi syarat >= 1.22)\n", goVer)

	// 2. Load & validasi file config
	log.Debug("memuat config", "path", g.config)
	c, err := config.Load(g.config)
	if err != nil {
		fmt.Fprintln(stderr, "[GAGAL] Validasi Config:")
		fmt.Fprintln(stderr, strings.TrimSpace(err.Error()))
		return 1
	}
	fmt.Fprintf(stdout, "[OK] Config terstruktur dengan benar (%d site terdaftar)\n", len(c.Sites))

	if configOnly {
		return 0
	}

	// 3. Inisialisasi Google Client menggunakan Service Account
	var gOpts []google.ClientOption
	gOpts = append(gOpts, google.WithLogger(log))
	if defaultHTTPClient != nil {
		gOpts = append(gOpts, google.WithHTTPClient(defaultHTTPClient))
	}

	gClient, err := google.NewClientFromFile(c.ServiceAccountFile, gOpts...)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Service Account Key: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "[OK] Service Account Key valid (PKCS#8 RSA)")

	// 4. Periksa akses ke Google Search Console (sites.list)
	ctx := context.Background()
	log.Debug("mengambil daftar properti situs dari GSC...")
	remoteSites, err := gClient.ListSites(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Akses GSC API: %v\n", err)
		fmt.Fprintln(stderr, "  -> Pastikan 'Google Search Console API' sudah di-ENABLE di Google Cloud Console")
		fmt.Fprintln(stderr, "  -> Pastikan akun Service Account sudah ditambahkan di GSC > Settings > Users & Permissions")
		return 1
	}
	fmt.Fprintf(stdout, "[OK] Berhasil terhubung ke GSC API (%d properti terdaftar pada Service Account)\n", len(remoteSites))

	// Peta properti terdaftar di GSC: siteUrl -> permissionLevel
	remoteMap := make(map[string]string)
	for _, s := range remoteSites {
		remoteMap[s.SiteURL] = s.PermissionLevel
	}

	// 5. Verifikasi izin tiap site di config
	hasError := false
	for _, site := range c.Sites {
		perm, found := remoteMap[site.GSCProperty]
		if !found {
			fmt.Fprintf(stderr, "[GAGAL] Site %q (%s): tidak ditemukan di daftar properti GSC\n", site.Name, site.GSCProperty)
			fmt.Fprintln(stderr, "  -> Periksa apakah format gsc_property sudah sama persis (mis. sc-domain:example.com atau https://example.com/ dengan trailing slash)")
			fmt.Fprintln(stderr, "  -> Pastikan email Service Account sudah diundang di properti ini pada Search Console")
			hasError = true
			continue
		}

		permDisplay := perm
		switch perm {
		case "siteOwner":
			permDisplay = "Owner (Akses Penuh)"
		case "siteFullUser":
			permDisplay = "Full (Dapat submit sitemap & inspect)"
		case "siteRestrictedUser":
			permDisplay = "Restricted (Hanya baca/inspect)"
		}
		fmt.Fprintf(stdout, "[OK] Site %q (%s) -> Izin: %s\n", site.Name, site.GSCProperty, permDisplay)
	}

	if hasError {
		fmt.Fprintln(stderr, "\nDaftar properti yang saat ini terdaftar pada akun Service Account:")
		if len(remoteSites) == 0 {
			fmt.Fprintln(stderr, "  (Belum ada properti apapun. Undang Service Account di Search Console Settings > Users)")
		} else {
			for _, s := range remoteSites {
				fmt.Fprintf(stderr, "  - %s (Izin: %s)\n", s.SiteURL, s.PermissionLevel)
			}
		}
		return 1
	}

	fmt.Fprintln(stdout, "\nSemua pemeriksaan doctor sukses! Tool siap digunakan.")
	return 0
}

func checkGoVersion(ver string) error {
	v := strings.TrimPrefix(ver, "go")
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		// Jika versi devel atau non-standar, lewati pengecekan ketat
		return nil
	}

	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return nil
	}

	if major < 1 || (major == 1 && minor < 22) {
		return fmt.Errorf("versi Go saat ini adalah %s, minimal membutuhkan Go 1.22+", ver)
	}
	return nil
}
