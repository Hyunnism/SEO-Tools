package sitemap

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	baseURL := "https://example.com"
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"valid standar", "https://example.com/blog/go", "https://example.com/blog/go", false},
		{"uppercase host", "https://EXAMPLE.COM/blog", "https://example.com/blog", false},
		{"buang fragment", "https://example.com/page#section", "https://example.com/page", false},
		{"buang default port 443", "https://example.com:443/page", "https://example.com/page", false},
		{"buang default port 80", "http://example.com:80/page", "http://example.com/page", false},
		{"path kosong jadi slash", "https://example.com", "https://example.com/", false},
		{"relative path", "/about", "https://example.com/about", false},
		{"host berbeda ditolak", "https://another-domain.com/page", "", true},
		{"skema tidak didukung", "ftp://example.com/page", "", true},
		{"url kosong", "   ", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeURL(tc.input, baseURL)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFetchAndParse_URLSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>http://`+r.Host+`/page1</loc></url>
  <url><loc>http://`+r.Host+`/page2#hash</loc></url>
  <url><loc>http://`+r.Host+`/admin/secret</loc></url>
  <url><loc>https://external-domain.com/other</loc></url>
</urlset>`)
	}))
	defer server.Close()

	ctx := context.Background()
	urls, err := FetchAndParse(ctx, server.Client(), server.URL+"/sitemap.xml", server.URL, []string{"/admin"}, 5000)
	if err != nil {
		t.Fatalf("FetchAndParse failed: %v", err)
	}

	if len(urls) != 2 {
		t.Fatalf("got %d urls (%v), want 2", len(urls), urls)
	}

	want1 := server.URL + "/page1"
	want2 := server.URL + "/page2"
	if urls[0] != want1 || urls[1] != want2 {
		t.Errorf("got %v, want [%s, %s]", urls, want1, want2)
	}
}

func TestFetchAndParse_SitemapIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		switch r.URL.Path {
		case "/sitemap_index.xml":
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>http://`+r.Host+`/sub_sitemap.xml</loc></sitemap>
</sitemapindex>`)
		case "/sub_sitemap.xml":
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>http://`+r.Host+`/from-sub</loc></url>
</urlset>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	urls, err := FetchAndParse(context.Background(), server.Client(), server.URL+"/sitemap_index.xml", server.URL, nil, 100)
	if err != nil {
		t.Fatalf("FetchAndParse sitemap index failed: %v", err)
	}

	if len(urls) != 1 || urls[0] != server.URL+"/from-sub" {
		t.Errorf("got %v, want [%s/from-sub]", urls, server.URL)
	}
}

func TestFetchAndParse_DepthLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		switch r.URL.Path {
		case "/level1.xml":
			fmt.Fprint(w, `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>http://`+r.Host+`/level2.xml</loc></sitemap></sitemapindex>`)
		case "/level2.xml":
			fmt.Fprint(w, `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>http://`+r.Host+`/level3.xml</loc></sitemap></sitemapindex>`)
		case "/level3.xml":
			fmt.Fprint(w, `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>http://`+r.Host+`/level4.xml</loc></sitemap></sitemapindex>`)
		case "/level4.xml":
			fmt.Fprint(w, `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>http://`+r.Host+`/level5.xml</loc></sitemap></sitemapindex>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := FetchAndParse(context.Background(), server.Client(), server.URL+"/level1.xml", server.URL, nil, 100)
	if err == nil {
		t.Fatal("harus gagal karena depth limit melebihi 3")
	}
}

func TestFetchAndParse_Gzip(t *testing.T) {
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-gzip")
		w.Write(gzBuf.Bytes())
	}))
	defer server.Close()
	serverURL = server.URL

	xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>` + serverURL + `/gzip-page</loc></url>
</urlset>`
	gw.Write([]byte(xmlContent))
	gw.Close()

	urls, err := FetchAndParse(context.Background(), server.Client(), server.URL+"/sitemap.xml.gz", server.URL, nil, 100)
	if err != nil {
		t.Fatalf("Gzip sitemap parse failed: %v", err)
	}

	if len(urls) != 1 || urls[0] != serverURL+"/gzip-page" {
		t.Errorf("got %v, want [%s/gzip-page]", urls, serverURL)
	}
}

func TestAutoDiscover(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			fmt.Fprint(w, "User-agent: *\nDisallow: /admin\nSitemap: https://example.com/auto-sitemap.xml\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	found, err := AutoDiscover(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("AutoDiscover failed: %v", err)
	}

	if len(found) != 1 || found[0] != "https://example.com/auto-sitemap.xml" {
		t.Errorf("got %v, want https://example.com/auto-sitemap.xml", found)
	}
}
