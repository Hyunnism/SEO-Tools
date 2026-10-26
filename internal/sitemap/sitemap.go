package sitemap

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const (
	MaxSitemapDepth    = 3
	MaxURLsPerSitemap  = 50000
	MaxDecompressedXML = 50 * 1024 * 1024 // 50 MB batas dekompresi untuk mencegah zip bomb
)

type urlset struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	} `xml:"url"`
}

type sitemapIndex struct {
	XMLName  xml.Name `xml:"sitemapindex"`
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

// NormalizeURL membersihkan URL: membuang fragment, port default, lowercase hostname,
// dan memastikan host sama persis dengan baseURL.
func NormalizeURL(rawURL string, baseURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", errors.New("URL kosong")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	b, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("baseURL tidak valid: %w", err)
	}

	// Tangani relative URL jika ada
	if !u.IsAbs() {
		u = b.ResolveReference(u)
	}

	// Normalisasi skema dan host
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("skema %q tidak didukung", u.Scheme)
	}

	u.Host = strings.ToLower(u.Host)
	u.Fragment = "" // buang fragment #...

	// Buang port default
	if (u.Scheme == "http" && strings.HasSuffix(u.Host, ":80")) ||
		(u.Scheme == "https" && strings.HasSuffix(u.Host, ":443")) {
		u.Host = strings.Split(u.Host, ":")[0]
	}

	bHost := strings.ToLower(b.Host)
	if (b.Scheme == "http" && strings.HasSuffix(bHost, ":80")) ||
		(b.Scheme == "https" && strings.HasSuffix(bHost, ":443")) {
		bHost = strings.Split(bHost, ":")[0]
	}

	// Pastikan host sama dengan host baseURL
	if u.Host != bHost {
		return "", fmt.Errorf("host %q tidak sama dengan baseURL host %q", u.Host, bHost)
	}

	if u.Path == "" {
		u.Path = "/"
	}

	return u.String(), nil
}

// FetchAndParse membaca URL sitemap (bisa urlset, sitemap index, atau .gz),
// dan mengembalikan daftar URL unik yang telah difilter dan dinormalisasi.
func FetchAndParse(ctx context.Context, client *http.Client, sitemapURL string, baseURL string, excludePaths []string, maxPages int) ([]string, error) {
	if client == nil {
		client = http.DefaultClient
	}

	seenSitemaps := make(map[string]bool)
	urlMap := make(map[string]bool)

	var parseRecursive func(u string, depth int) error
	parseRecursive = func(u string, depth int) error {
		if depth > MaxSitemapDepth {
			return fmt.Errorf("kedalaman sitemap index melebihi batas maksimal (%d)", MaxSitemapDepth)
		}

		normSitemap, err := NormalizeURL(u, baseURL)
		if err != nil {
			return fmt.Errorf("URL sitemap %q tidak valid: %w", u, err)
		}

		if seenSitemaps[normSitemap] {
			return nil // Hindari loop sitemap
		}
		seenSitemaps[normSitemap] = true

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, normSitemap, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept-Encoding", "gzip")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("gagal mengambil sitemap %s: %w", normSitemap, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("sitemap %s mengembalikan status HTTP %d", normSitemap, resp.StatusCode)
		}

		var reader io.Reader = resp.Body

		// Cek apakah response berupa gzip (ekstensi .gz atau Content-Encoding: gzip)
		isGzip := strings.HasSuffix(strings.ToLower(u), ".gz") ||
			strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip")

		if isGzip {
			gz, err := gzip.NewReader(resp.Body)
			if err != nil {
				return fmt.Errorf("gagal dekompresi gzip sitemap: %w", err)
			}
			defer gz.Close()
			reader = gz
		}

		// Batasi pembacaan dekompresi untuk mencegah gzip bomb
		limitedReader := io.LimitReader(reader, MaxDecompressedXML)
		data, err := io.ReadAll(limitedReader)
		if err != nil {
			return fmt.Errorf("gagal membaca isi sitemap: %w", err)
		}

		// 1. Coba decode sebagai sitemapindex
		var sIdx sitemapIndex
		if err := xml.Unmarshal(data, &sIdx); err == nil && len(sIdx.Sitemaps) > 0 {
			for _, sub := range sIdx.Sitemaps {
				if err := parseRecursive(sub.Loc, depth+1); err != nil {
					return err
				}
			}
			return nil
		}

		// 2. Decode sebagai urlset
		var uSet urlset
		if err := xml.Unmarshal(data, &uSet); err != nil {
			return fmt.Errorf("gagal mengurai XML sitemap %s: %w", normSitemap, err)
		}

		for _, item := range uSet.URLs {
			if len(urlMap) >= maxPages && maxPages > 0 {
				break
			}
			norm, err := NormalizeURL(item.Loc, baseURL)
			if err != nil {
				continue // Lewati URL yang host-nya berbeda atau tidak valid
			}

			if isExcluded(norm, excludePaths) {
				continue
			}

			urlMap[norm] = true
		}

		return nil
	}

	if err := parseRecursive(sitemapURL, 1); err != nil {
		return nil, err
	}

	result := make([]string, 0, len(urlMap))
	for u := range urlMap {
		result = append(result, u)
	}
	sort.Strings(result)

	return result, nil
}

func isExcluded(rawURL string, excludePaths []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	for _, p := range excludePaths {
		if strings.HasPrefix(u.Path, p) {
			return true
		}
	}
	return false
}

// AutoDiscover mencari lokasi sitemap otomatis dari robots.txt atau fallback standard.
func AutoDiscover(ctx context.Context, client *http.Client, baseURL string) ([]string, error) {
	if client == nil {
		client = http.DefaultClient
	}

	b, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	// 1. Coba robots.txt
	robotsURL := fmt.Sprintf("%s://%s/robots.txt", b.Scheme, b.Host)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	resp, err := client.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var sitemaps []string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(strings.ToLower(line), "sitemap:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					sm := strings.TrimSpace(parts[1])
					if sm != "" {
						sitemaps = append(sitemaps, sm)
					}
				}
			}
		}
		if len(sitemaps) > 0 {
			return sitemaps, nil
		}
	} else if resp != nil {
		resp.Body.Close()
	}

	// 2. Fallback probe default
	fallbacks := []string{
		fmt.Sprintf("%s://%s/sitemap.xml", b.Scheme, b.Host),
		fmt.Sprintf("%s://%s/sitemap_index.xml", b.Scheme, b.Host),
	}

	for _, fb := range fallbacks {
		req, _ := http.NewRequestWithContext(ctx, http.MethodHead, fb, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return []string{fb}, nil
			}
		}
	}

	return nil, errors.New("tidak menemukan sitemap di robots.txt maupun path standar /sitemap.xml")
}
