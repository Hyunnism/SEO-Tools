package google

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// generateTestRSAKey membuat key RSA temporary (2048-bit) dalam format PEM PKCS#8
func generateTestRSAKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gagal membuat rsa key: %v", err)
	}

	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(pk)
	if err != nil {
		t.Fatalf("gagal marshal pkcs8: %v", err)
	}

	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8Bytes,
	})

	return pk, string(pemBlock)
}

func TestJWTSigningAndToken(t *testing.T) {
	pk, pemStr := generateTestRSAKey(t)

	var receivedAssertion string
	var tokenCalls int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenCalls, 1)
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			http.Error(w, "invalid grant_type", http.StatusBadRequest)
			return
		}

		receivedAssertion = r.Form.Get("assertion")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"ya29.test-mock-token","expires_in":3600}`)
	}))
	defer ts.Close()

	saKey := ServiceAccountKey{
		ClientEmail: "test-bot@example.iam.gserviceaccount.com",
		PrivateKey:  pemStr,
		TokenURI:    ts.URL,
	}
	saJSON, err := json.Marshal(saKey)
	if err != nil {
		t.Fatal(err)
	}

	client, err := NewClientFromBytes(saJSON)
	if err != nil {
		t.Fatalf("NewClientFromBytes failed: %v", err)
	}

	ctx := context.Background()
	tok, err := client.Token(ctx, ScopeWebmastersReadonly)
	if err != nil {
		t.Fatalf("Token failed: %v", err)
	}

	if tok != "ya29.test-mock-token" {
		t.Errorf("got token %q, want ya29.test-mock-token", tok)
	}

	// Verifikasi JWT Assertion
	parts := strings.Split(receivedAssertion, ".")
	if len(parts) != 3 {
		t.Fatalf("assertion tidak memiliki 3 bagian: %s", receivedAssertion)
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header failed: %v", err)
	}
	var header map[string]string
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		t.Fatalf("unmarshal header failed: %v", err)
	}
	if header["alg"] != "RS256" || header["typ"] != "JWT" {
		t.Errorf("header salah: %+v", header)
	}

	claimBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims failed: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimBytes, &claims); err != nil {
		t.Fatalf("unmarshal claims failed: %v", err)
	}
	if claims["iss"] != saKey.ClientEmail {
		t.Errorf("claim iss salah: %v", claims["iss"])
	}
	if claims["scope"] != ScopeWebmastersReadonly {
		t.Errorf("claim scope salah: %v", claims["scope"])
	}
	if claims["aud"] != ts.URL {
		t.Errorf("claim aud salah: %v", claims["aud"])
	}

	// Verifikasi Signature dengan RSA Public Key
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode sig failed: %v", err)
	}
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&pk.PublicKey, crypto.SHA256, hash[:], sigBytes); err != nil {
		t.Fatalf("verifikasi signature gagal: %v", err)
	}
}

func TestTokenCachingAndRefresh(t *testing.T) {
	_, pemStr := generateTestRSAKey(t)
	var tokenCalls int32
	var expiresIn int32 = 3600

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":%d}`, atomic.LoadInt32(&tokenCalls), atomic.LoadInt32(&expiresIn))
	}))
	defer ts.Close()

	saKey := ServiceAccountKey{
		ClientEmail: "test-bot@example.iam.gserviceaccount.com",
		PrivateKey:  pemStr,
		TokenURI:    ts.URL,
	}
	saJSON, _ := json.Marshal(saKey)
	client, err := NewClientFromBytes(saJSON)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Panggilan 1: request token ke server
	tok1, err := client.Token(ctx, ScopeWebmastersReadonly)
	if err != nil {
		t.Fatal(err)
	}
	if tok1 != "token-1" {
		t.Fatalf("tok1 = %s, want token-1", tok1)
	}
	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Fatalf("calls = %d, want 1", tokenCalls)
	}

	// Panggilan 2: harus diambil dari cache
	tok2, err := client.Token(ctx, ScopeWebmastersReadonly)
	if err != nil {
		t.Fatal(err)
	}
	if tok2 != "token-1" {
		t.Fatalf("tok2 = %s, want token-1 dari cache", tok2)
	}
	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Fatalf("calls = %d, harus tetap 1 (cache hit)", tokenCalls)
	}

	// Simulasi token hampir expired (misal < 60 detik)
	client.mu.Lock()
	cached := client.cache[ScopeWebmastersReadonly]
	cached.exp = time.Now().Add(30 * time.Second) // tersisa 30s (< 60s)
	client.cache[ScopeWebmastersReadonly] = cached
	client.mu.Unlock()

	// Panggilan 3: harus refresh karena tersisa < 60s
	tok3, err := client.Token(ctx, ScopeWebmastersReadonly)
	if err != nil {
		t.Fatal(err)
	}
	if tok3 != "token-2" {
		t.Fatalf("tok3 = %s, want token-2 setelah refresh", tok3)
	}
	if atomic.LoadInt32(&tokenCalls) != 2 {
		t.Fatalf("calls = %d, want 2 setelah refresh", tokenCalls)
	}
}

func TestDoJSONRetryOn429And5xx(t *testing.T) {
	_, pemStr := generateTestRSAKey(t)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"valid-token","expires_in":3600}`)
	}))
	defer tokenServer.Close()

	var apiAttempts int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(&apiAttempts, 1)
		auth := r.Header.Get("Authorization")
		if auth != "Bearer valid-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if attempt == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests) // 429
			return
		}
		if attempt == 2 {
			http.Error(w, "Server error", http.StatusInternalServerError) // 500
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"sukses"}`)
	}))
	defer apiServer.Close()

	saKey := ServiceAccountKey{
		ClientEmail: "test-bot@example.iam.gserviceaccount.com",
		PrivateKey:  pemStr,
		TokenURI:    tokenServer.URL,
	}
	saJSON, _ := json.Marshal(saKey)

	client, err := NewClientFromBytes(
		saJSON,
		WithRetrySettings(10*time.Millisecond, 4), // delay cepat untuk unit test
	)
	if err != nil {
		t.Fatal(err)
	}

	var resp struct {
		Status string `json:"status"`
	}
	err = client.DoJSON(context.Background(), http.MethodGet, apiServer.URL, ScopeWebmastersReadonly, nil, &resp)
	if err != nil {
		t.Fatalf("DoJSON failed: %v", err)
	}

	if resp.Status != "sukses" {
		t.Errorf("resp.Status = %s, want sukses", resp.Status)
	}
	if atomic.LoadInt32(&apiAttempts) != 3 {
		t.Errorf("apiAttempts = %d, want 3", apiAttempts)
	}
}

func TestDoJSONNoRetryOn400Or404(t *testing.T) {
	_, pemStr := generateTestRSAKey(t)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"valid-token","expires_in":3600}`)
	}))
	defer tokenServer.Close()

	var apiAttempts int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&apiAttempts, 1)
		http.Error(w, "Bad Request", http.StatusBadRequest)
	}))
	defer apiServer.Close()

	saKey := ServiceAccountKey{
		ClientEmail: "test-bot@example.iam.gserviceaccount.com",
		PrivateKey:  pemStr,
		TokenURI:    tokenServer.URL,
	}
	saJSON, _ := json.Marshal(saKey)
	client, err := NewClientFromBytes(saJSON, WithRetrySettings(10*time.Millisecond, 4))
	if err != nil {
		t.Fatal(err)
	}

	err = client.DoJSON(context.Background(), http.MethodGet, apiServer.URL, ScopeWebmastersReadonly, nil, nil)
	if err == nil {
		t.Fatal("harus gagal pada 400 Bad Request")
	}
	if atomic.LoadInt32(&apiAttempts) != 1 {
		t.Errorf("attempts = %d, status 400 tidak boleh di-retry", apiAttempts)
	}
}

func TestAuthorizationHeaderNotLogged(t *testing.T) {
	_, pemStr := generateTestRSAKey(t)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"super-secret-token-12345","expires_in":3600}`)
	}))
	defer tokenServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{}`)
	}))
	defer apiServer.Close()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	saKey := ServiceAccountKey{
		ClientEmail: "test-bot@example.iam.gserviceaccount.com",
		PrivateKey:  pemStr,
		TokenURI:    tokenServer.URL,
	}
	saJSON, _ := json.Marshal(saKey)
	client, err := NewClientFromBytes(saJSON, WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}

	_ = client.DoJSON(context.Background(), http.MethodGet, apiServer.URL, ScopeWebmastersReadonly, nil, nil)

	logOutput := logBuf.String()
	if strings.Contains(logOutput, "super-secret-token-12345") {
		t.Fatalf("KEBOCORAN KEAMANAN: Token rahasia ditemukan dalam log: %s", logOutput)
	}
	if strings.Contains(strings.ToLower(logOutput), "bearer") {
		t.Fatalf("KEBOCORAN KEAMANAN: Header Bearer ditemukan dalam log: %s", logOutput)
	}
}

func TestTokenConcurrentAccess(t *testing.T) {
	_, pemStr := generateTestRSAKey(t)
	var tokenCalls int32

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenCalls, 1)
		time.Sleep(10 * time.Millisecond) // simulasi latency
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"concurrent-token","expires_in":3600}`)
	}))
	defer tokenServer.Close()

	saKey := ServiceAccountKey{
		ClientEmail: "test-bot@example.iam.gserviceaccount.com",
		PrivateKey:  pemStr,
		TokenURI:    tokenServer.URL,
	}
	saJSON, _ := json.Marshal(saKey)
	client, err := NewClientFromBytes(saJSON)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	ctx := context.Background()

	// 20 goroutine memanggil Token bersamaan
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := client.Token(ctx, ScopeWebmastersReadonly)
			if err != nil {
				t.Errorf("Token() error: %v", err)
			}
			if tok != "concurrent-token" {
				t.Errorf("got %s, want concurrent-token", tok)
			}
		}()
	}
	wg.Wait()
}

func TestListSites(t *testing.T) {
	_, pemStr := generateTestRSAKey(t)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"test-token","expires_in":3600}`)
	}))
	defer tokenServer.Close()

	mockGSC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/webmasters/v3/sites" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"siteEntry": [
				{"siteUrl": "sc-domain:example.com", "permissionLevel": "siteOwner"},
				{"siteUrl": "https://example.com/blog/", "permissionLevel": "siteFullUser"}
			]
		}`)
	}))
	defer mockGSC.Close()

	saKey := ServiceAccountKey{
		ClientEmail: "test-bot@example.iam.gserviceaccount.com",
		PrivateKey:  pemStr,
		TokenURI:    tokenServer.URL,
	}
	saJSON, _ := json.Marshal(saKey)

	// Ubah endpoint untuk test dengan custom transport
	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "www.googleapis.com" {
			req.URL.Scheme = "http"
			req.URL.Host = mockGSC.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	httpClient := &http.Client{Transport: customTransport}
	client, err := NewClientFromBytes(saJSON, WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}

	sites, err := client.ListSites(context.Background())
	if err != nil {
		t.Fatalf("ListSites gagal: %v", err)
	}

	if len(sites) != 2 {
		t.Fatalf("got %d sites, want 2", len(sites))
	}
	if sites[0].SiteURL != "sc-domain:example.com" || sites[0].PermissionLevel != "siteOwner" {
		t.Errorf("site 0 salah: %+v", sites[0])
	}
	if sites[1].SiteURL != "https://example.com/blog/" || sites[1].PermissionLevel != "siteFullUser" {
		t.Errorf("site 1 salah: %+v", sites[1])
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
