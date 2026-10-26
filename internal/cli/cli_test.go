package cli

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"seo-boost/internal/google"
)

func run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb, "dev")
	return code, out.String(), errb.String()
}

func generateTestRSAKeyPEM(t *testing.T) string {
	t.Helper()
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	b, err := x509.MarshalPKCS8PrivateKey(pk)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}))
}

func TestVersion(t *testing.T) {
	code, out, _ := run("version")
	if code != 0 || strings.TrimSpace(out) != "dev" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestNoArgs(t *testing.T) {
	code, _, errs := run()
	if code != 1 || !strings.Contains(errs, "Pemakaian") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, errs := run("ngawur")
	if code != 1 || !strings.Contains(errs, "ngawur") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestNotYetAvailable(t *testing.T) {
	code, _, errs := run("crawl")
	if code != 1 || !strings.Contains(errs, "belum tersedia") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestHelp(t *testing.T) {
	for _, a := range []string{"help", "--help", "-h"} {
		code, out, _ := run(a)
		if code != 0 || !strings.Contains(out, "Pemakaian") {
			t.Fatalf("%s: code=%d out=%q", a, code, out)
		}
	}
}

func TestDoctorConfigOnly(t *testing.T) {
	dir := t.TempDir()
	sa := `{"client_email":"a","private_key":"k","token_uri":"u"}`
	good := `{"service_account_file":"./sa.json","sites":[{"name":"a","gsc_property":"sc-domain:e.com","base_url":"https://e.com"}]}`
	bad := `{"service_account_file":"./sa.json","sites":[]}`
	for name, body := range map[string]string{"sa.json": sa, "good.json": good, "bad.json": bad} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if code, out, _ := run("doctor", "--config-only", "--config", filepath.Join(dir, "good.json")); code != 0 || !strings.Contains(out, "Config terstruktur dengan benar") {
		t.Fatalf("good: code=%d out=%q", code, out)
	}
	if code, _, errs := run("doctor", "--config-only", "--config", filepath.Join(dir, "bad.json")); code != 1 || !strings.Contains(errs, "minimal 1 site") {
		t.Fatalf("bad: code=%d err=%q", code, errs)
	}
	if code, _, errs := run("doctor", "--config-only", "--config", filepath.Join(dir, "tidak-ada.json")); code != 1 || errs == "" {
		t.Fatalf("missing: code=%d err=%q", code, errs)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestDoctorOnlineMock(t *testing.T) {
	pemKey := generateTestRSAKeyPEM(t)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"mock-token","expires_in":3600}`)
		case "/webmasters/v3/sites":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{
				"siteEntry": [
					{"siteUrl": "sc-domain:example.com", "permissionLevel": "siteOwner"}
				]
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "www.googleapis.com" {
			req.URL.Scheme = "http"
			req.URL.Host = mockServer.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	defaultHTTPClient = &http.Client{Transport: customTransport}
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	sa := google.ServiceAccountKey{
		ClientEmail: "test-bot@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    mockServer.URL + "/token",
	}
	saBytes, _ := json.Marshal(sa)
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), saBytes, 0o600)

	// Kasus 1: Site cocok dengan properti di GSC
	cfgGood := `{"service_account_file":"./sa.json","sites":[{"name":"portfolio","gsc_property":"sc-domain:example.com","base_url":"https://example.com"}]}`
	cfgGoodPath := filepath.Join(dir, "good.json")
	_ = os.WriteFile(cfgGoodPath, []byte(cfgGood), 0o600)

	code, out, _ := run("doctor", "--config", cfgGoodPath)
	if code != 0 {
		t.Fatalf("expected doctor sukses, got %d, out: %s", code, out)
	}
	if !strings.Contains(out, "Owner (Akses Penuh)") {
		t.Errorf("output tidak memuat Owner: %s", out)
	}

	// Kasus 2: Site tidak ada di GSC
	cfgMissing := `{"service_account_file":"./sa.json","sites":[{"name":"blog","gsc_property":"sc-domain:tidak-ada.com","base_url":"https://tidak-ada.com"}]}`
	cfgMissingPath := filepath.Join(dir, "missing.json")
	_ = os.WriteFile(cfgMissingPath, []byte(cfgMissing), 0o600)

	code, _, errs := run("doctor", "--config", cfgMissingPath)
	if code != 1 {
		t.Fatalf("expected doctor gagal karena site tidak terdaftar di GSC, got code %d", code)
	}
	if !strings.Contains(errs, "tidak ditemukan di daftar properti GSC") {
		t.Errorf("error output salah: %s", errs)
	}
}

func TestCheckGoVersion(t *testing.T) {
	if err := checkGoVersion("go1.21.0"); err == nil {
		t.Error("go1.21.0 harus gagal")
	}
	if err := checkGoVersion("go1.22.0"); err != nil {
		t.Errorf("go1.22.0 harus lolos: %v", err)
	}
	if err := checkGoVersion("go1.25.7"); err != nil {
		t.Errorf("go1.25.7 harus lolos: %v", err)
	}
}

func TestVerboseLogger(t *testing.T) {
	var b bytes.Buffer
	if newLogger(&b, false).Enabled(nil, slog.LevelDebug) {
		t.Fatal("debug harus mati tanpa --verbose")
	}
	if !newLogger(&b, true).Enabled(nil, slog.LevelDebug) {
		t.Fatal("debug harus nyala dengan --verbose")
	}
}
