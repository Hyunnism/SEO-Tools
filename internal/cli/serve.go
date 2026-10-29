package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"seo-boost/internal/config"
	"seo-boost/internal/serve"
	"seo-boost/internal/store"
)

func serveCmd(args []string, stdout, stderr io.Writer) int {
	return serveCmdContext(context.Background(), args, stdout, stderr)
}

func serveCmdContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)

	addr := "127.0.0.1:8080"
	token := ""
	noAuth := false
	openBrowser := false

	fs.StringVar(&addr, "addr", "127.0.0.1:8080", "alamat bind server lokal (contoh: 127.0.0.1:8080)")
	fs.StringVar(&token, "token", "", "token keamanan akses dashboard (jika kosong, dibuat acak)")
	fs.BoolVar(&noAuth, "no-auth", false, "matikan autentikasi token (hanya berlaku pada antarmuka loopback)")
	fs.BoolVar(&openBrowser, "open", false, "buka dashboard di browser web default saat mulai")

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

	requireAuth := !noAuth
	if requireAuth && token == "" {
		rb := make([]byte, 16)
		if _, rErr := rand.Read(rb); rErr != nil {
			fmt.Fprintf(stderr, "[GAGAL] Buat token acak: %v\n", rErr)
			return 1
		}
		token = hex.EncodeToString(rb)
	}

	if err := serve.ValidateAddr(addr, token, requireAuth); err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Konfigurasi server tidak aman: %v\n", err)
		return 1
	}

	log := newLogger(stderr, g.verbose)
	st := store.NewStore(c.DataDir)

	runner := func(ctx context.Context, site *config.Site, progress serve.ProgressCallback) error {
		unlock, lErr := st.Lock(site.Name)
		if lErr != nil {
			return fmt.Errorf("gagal mengunci store situs %s: %w", site.Name, lErr)
		}
		defer unlock()

		opts := defaultPipelineOptions()
		_, _, _, _, pErr := executePipeline(ctx, c, site, opts, log, st, progress, nil)
		return pErr
	}

	srv := serve.NewServer(c, st, runner, token, requireAuth)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv.Handler(),
	}

	accessURL := fmt.Sprintf("http://%s/", addr)
	if requireAuth && token != "" {
		accessURL = fmt.Sprintf("http://%s/?token=%s", addr, token)
	}

	fmt.Fprintf(stdout, "================================================================================\n")
	fmt.Fprintf(stdout, "  SEO-BOOST WEB DASHBOARD\n")
	fmt.Fprintf(stdout, "================================================================================\n")
	fmt.Fprintf(stdout, "  [INFO] Server mendengarkan pada : http://%s\n", addr)
	fmt.Fprintf(stdout, "  [INFO] URL Akses Langsung      : %s\n", accessURL)
	if requireAuth {
		fmt.Fprintf(stdout, "  [INFO] Token Keamanan          : %s\n", token)
	} else {
		fmt.Fprintf(stdout, "  [WARN] Autentikasi token dinonaktifkan (--no-auth).\n")
	}
	fmt.Fprintf(stdout, "  [INFO] Tekan Ctrl+C untuk menghentikan server.\n\n")

	if openBrowser {
		go launchBrowser(accessURL)
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		fmt.Fprintf(stderr, "[GAGAL] Server HTTP error: %v\n", err)
		return 1
	case <-ctx.Done():
		fmt.Fprintf(stdout, "\n[INFO] Konteks dibatalkan, mematikan server secara anggun...\n")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(stderr, "[PERINGATAN] Gagal mematikan server secara bersih: %v\n", err)
		}
		fmt.Fprintf(stdout, "[INFO] Server berhenti.\n")
		return 0
	case sig := <-sigChan:
		fmt.Fprintf(stdout, "\n[INFO] Menerima sinyal %v, mematikan server secara anggun...\n", sig)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(stderr, "[PERINGATAN] Gagal mematikan server secara bersih: %v\n", err)
		}
		fmt.Fprintf(stdout, "[INFO] Server berhenti.\n")
		return 0
	}
}

func launchBrowser(url string) {
	time.Sleep(300 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
