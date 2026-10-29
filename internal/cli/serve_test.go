package cli

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeCmd_Help(t *testing.T) {
	code, out, errs := run("serve", "--help")
	if code != 0 {
		t.Fatalf("diharapkan exit code 0 untuk --help, dapat: %d", code)
	}
	combined := out + errs
	if !strings.Contains(combined, "-addr") || !strings.Contains(combined, "-token") {
		t.Errorf("output help tidak mengandung flag yang diharapkan: %s", combined)
	}
}

func TestServeCmd_InvalidConfig(t *testing.T) {
	code, _, errs := run("serve", "--config", "nonexistent-config.json")
	if code != 1 {
		t.Fatalf("diharapkan exit code 1 untuk config tidak valid, dapat: %d", code)
	}
	if !strings.Contains(errs, "[GAGAL]") {
		t.Errorf("diharapkan pesan kegagalan di stderr: %s", errs)
	}
}

func TestServeCmd_InsecureNonLoopbackRejected(t *testing.T) {
	tmpDir := t.TempDir()
	saPath := filepath.Join(tmpDir, "sa.json")
	_ = os.WriteFile(saPath, []byte(`{"client_email":"a@b.com","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o644)

	cfgPath := filepath.Join(tmpDir, "config.json")
	cfgContent := `{
		"service_account_file": "` + filepath.ToSlash(saPath) + `",
		"data_dir": "` + filepath.ToSlash(tmpDir) + `",
		"sites": [{"name": "site-a", "base_url": "https://example.com", "gsc_property": "sc-domain:example.com"}]
	}`
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o644)

	// Non-loopback dengan --no-auth harus ditolak
	code, _, errs := run("serve", "--config", cfgPath, "--addr", "0.0.0.0:9999", "--no-auth")
	if code != 1 {
		t.Fatalf("diharapkan exit code 1 untuk non-loopback tanpa auth, dapat: %d", code)
	}
	if !strings.Contains(errs, "tidak aman") {
		t.Errorf("pesan error tidak sesuai: %s", errs)
	}
}

func TestServeCmd_StartupAndGracefulShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	saPath := filepath.Join(tmpDir, "sa.json")
	_ = os.WriteFile(saPath, []byte(`{"client_email":"a@b.com","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o644)

	cfgPath := filepath.Join(tmpDir, "config.json")
	cfgContent := `{
		"service_account_file": "` + filepath.ToSlash(saPath) + `",
		"data_dir": "` + filepath.ToSlash(tmpDir) + `",
		"sites": [{"name": "site-a", "base_url": "https://example.com", "gsc_property": "sc-domain:example.com"}]
	}`
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o644)

	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)

	// Gunakan port loopback dinamis / tinggi untuk pengujian
	testAddr := "127.0.0.1:18089"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		code := serveCmdContext(ctx, []string{"--config", cfgPath, "--addr", testAddr, "--token", "test-token-abc"}, &stdout, &stderr)
		done <- code
	}()

	// Tunggu server mulai mendengarkan
	baseURL := "http://" + testAddr
	var clientOK bool
	for i := 0; i < 25; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(baseURL + "/?token=test-token-abc")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				clientOK = true
				break
			}
		}
	}

	if !clientOK {
		t.Fatalf("server tidak merespons request HTTP pada %s dalam 2.5s (stdout: %s, stderr: %s)", baseURL, stdout.String(), stderr.String())
	}

	// Uji endpoint API terautentikasi
	apiReq, _ := http.NewRequest("GET", baseURL+"/api/sites", nil)
	apiReq.Header.Set("Authorization", "Bearer test-token-abc")
	apiResp, err := http.DefaultClient.Do(apiReq)
	if err != nil || apiResp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/sites gagal: err=%v status=%v", err, apiResp.StatusCode)
	}
	if apiResp != nil {
		apiResp.Body.Close()
	}

	// Batalkan context untuk trigger graceful shutdown
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("diharapkan exit code 0 saat dihentikan anggun, dapat: %d (stderr: %s)", code, stderr.String())
		}
	case <-time.After(3 * time.Second):
		t.Errorf("server gagal shutdown dalam batas waktu 3 detik")
	}
}
