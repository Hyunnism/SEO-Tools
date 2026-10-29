package gsc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"seo-boost/internal/google"
)

func generateTestRSAKeyPEM(t *testing.T) string {
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
	return string(pemBlock)
}

func TestSubmitSitemap(t *testing.T) {
	pemKey := generateTestRSAKeyPEM(t)

	var lastMethod string
	var lastPath string
	var lastAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-access-token",
				"expires_in":   3600,
			})
		default:
			lastMethod = r.Method
			lastPath = r.URL.EscapedPath()
			lastAuth = r.Header.Get("Authorization")

			if strings.Contains(r.URL.Path, "invalid-site") {
				http.Error(w, `{"error":{"message":"Site not found"}}`, http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(req)
	})

	sa := google.ServiceAccountKey{
		ClientEmail: "tester@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    server.URL + "/token",
	}
	saBytes, _ := json.Marshal(sa)

	client, err := google.NewClientFromBytes(saBytes, google.WithHTTPClient(&http.Client{Transport: customTransport}))
	if err != nil {
		t.Fatalf("gagal inisialisasi client: %v", err)
	}

	ctx := context.Background()
	siteProp := "sc-domain:example.com"
	sitemapURL := "https://example.com/sitemap.xml"

	// 1. Submit sukses
	err = SubmitSitemap(ctx, client, siteProp, sitemapURL)
	if err != nil {
		t.Fatalf("SubmitSitemap error: %v", err)
	}

	if lastMethod != "PUT" {
		t.Errorf("expected PUT method, got %s", lastMethod)
	}
	if lastAuth != "Bearer mock-access-token" {
		t.Errorf("expected bearer token auth, got %s", lastAuth)
	}

	// Verifikasi URL-encoding ganda path: site dan feedpath
	expectedEscapedSite := url.PathEscape(siteProp)
	expectedEscapedFeed := url.PathEscape(sitemapURL)
	expectedSuffix := fmt.Sprintf("/sites/%s/sitemaps/%s", expectedEscapedSite, expectedEscapedFeed)
	if !strings.HasSuffix(lastPath, expectedSuffix) {
		t.Errorf("expected path to end with %s, got %s", expectedSuffix, lastPath)
	}

	// 2. Submit gagal (404)
	err = SubmitSitemap(ctx, client, "sc-domain:invalid-site", sitemapURL)
	if err == nil {
		t.Fatalf("expected error on invalid site, got nil")
	}
}

func TestListSitemaps(t *testing.T) {
	pemKey := generateTestRSAKeyPEM(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-access-token",
				"expires_in":   3600,
			})
		default:
			w.Header().Set("Content-Type", "application/json")
			now := time.Now().UTC()
			resp := sitemapsListResponse{
				Sitemap: []SitemapStatus{
					{
						Path:           "https://example.com/sitemap.xml",
						LastSubmitted:  &now,
						IsPending:      false,
						LastDownloaded: &now,
						Warnings:       "0",
						Errors:         "0",
						Contents: []SitemapContent{
							{
								Type:      "web",
								Submitted: "25",
								Indexed:   "24",
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(req)
	})

	sa := google.ServiceAccountKey{
		ClientEmail: "tester@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    server.URL + "/token",
	}
	saBytes, _ := json.Marshal(sa)

	client, err := google.NewClientFromBytes(saBytes, google.WithHTTPClient(&http.Client{Transport: customTransport}))
	if err != nil {
		t.Fatalf("gagal inisialisasi client: %v", err)
	}

	ctx := context.Background()
	sitemaps, err := ListSitemaps(ctx, client, "sc-domain:example.com")
	if err != nil {
		t.Fatalf("ListSitemaps error: %v", err)
	}

	if len(sitemaps) != 1 {
		t.Fatalf("expected 1 sitemap status, got %d", len(sitemaps))
	}
	sm := sitemaps[0]
	if sm.Path != "https://example.com/sitemap.xml" {
		t.Errorf("unexpected path: %s", sm.Path)
	}
	if len(sm.Contents) != 1 || sm.Contents[0].Indexed != "24" {
		t.Errorf("unexpected contents: %+v", sm.Contents)
	}
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
