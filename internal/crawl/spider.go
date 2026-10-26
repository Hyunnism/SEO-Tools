package crawl

import (
	"bufio"
	"context"
	"net/http"
	"net/url"
	"strings"

	"seo-boost/internal/extract"
	"seo-boost/internal/sitemap"
)

// SpiderBFS melakukan penelusuran internal link secara BFS mulai dari baseURL.
func SpiderBFS(ctx context.Context, client *http.Client, baseURL string, userAgent string, maxPages int) ([]string, error) {
	if client == nil {
		client = http.DefaultClient
	}

	if normBase, err := sitemap.NormalizeURL(baseURL, baseURL); err == nil {
		baseURL = normBase
	}

	disallowed := fetchRobotsDisallowed(ctx, client, baseURL)

	queue := []string{baseURL}
	seen := make(map[string]bool)
	seen[baseURL] = true

	var discovered []string

	for len(queue) > 0 {
		if maxPages > 0 && len(discovered) >= maxPages {
			break
		}

		select {
		case <-ctx.Done():
			return discovered, ctx.Err()
		default:
		}

		currURL := queue[0]
		queue = queue[1:]

		if isRobotsDisallowed(currURL, disallowed) {
			continue
		}

		discovered = append(discovered, currURL)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currURL, nil)
		if err != nil {
			continue
		}
		if userAgent != "" {
			req.Header.Set("User-Agent", userAgent)
		}
		req.Header.Set("Accept", "text/html, application/xhtml+xml")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}

		contentType := resp.Header.Get("Content-Type")
		if resp.StatusCode != http.StatusOK || (!strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "xhtml")) {
			resp.Body.Close()
			continue
		}

		_, links, err := extract.ExtractHTML(resp.Body, currURL)
		resp.Body.Close()
		if err != nil {
			continue
		}

		for _, link := range links {
			norm, err := sitemap.NormalizeURL(link, baseURL)
			if err != nil {
				continue // abaikan link keluar / beda host
			}

			if !seen[norm] {
				seen[norm] = true
				queue = append(queue, norm)
			}
		}
	}

	return discovered, nil
}

func fetchRobotsDisallowed(ctx context.Context, client *http.Client, baseURL string) []string {
	b, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}
	robotsURL := fmtURL(b.Scheme, b.Host, "/robots.txt")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	var disallowed []string
	scanner := bufio.NewScanner(resp.Body)
	appliesToAll := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		directive := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		if directive == "user-agent" {
			appliesToAll = (val == "*")
		} else if appliesToAll && directive == "disallow" && val != "" {
			disallowed = append(disallowed, val)
		}
	}
	return disallowed
}

func isRobotsDisallowed(rawURL string, disallowed []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	for _, dis := range disallowed {
		if strings.HasPrefix(u.Path, dis) {
			return true
		}
	}
	return false
}

func fmtURL(scheme, host, path string) string {
	return scheme + "://" + host + path
}
