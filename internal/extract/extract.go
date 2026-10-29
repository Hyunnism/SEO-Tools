package extract

import (
	"io"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"seo-boost/internal/model"
)

// ExtractHTML mengekstrak elemen-elemen SEO on-page dan mengumpulkan internal/external links.
// ponytail: menggunakan x/net/html parser standar Go; upgrade ke tokenizer HTML mandiri jika ingin zero external dependency murni.
func ExtractHTML(r io.Reader, pageURL string) (*model.CrawlInfo, []string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, nil, err
	}

	info := &model.CrawlInfo{}
	var links []string
	var bodyTextBuilder strings.Builder
	var scriptCount int
	var hasSpaRoot bool

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Noscript, atom.Template:
				if n.DataAtom == atom.Script {
					scriptCount++
					// Cek JSON-LD
					for _, attr := range n.Attr {
						if strings.EqualFold(attr.Key, "type") && strings.Contains(strings.ToLower(attr.Val), "ld+json") {
							info.HasJSONLD = true
						}
					}
				}
				// Jangan telusuri anak dari script/style/noscript/template
				return

			case atom.Title:
				t := extractText(n)
				if info.Title == "" {
					info.Title = t
					info.Zones.Title = t
				}

			case atom.Meta:
				var name, property, content string
				for _, attr := range n.Attr {
					key := strings.ToLower(attr.Key)
					if key == "name" {
						name = strings.ToLower(attr.Val)
					} else if key == "property" {
						property = strings.ToLower(attr.Val)
					} else if key == "content" {
						content = strings.TrimSpace(attr.Val)
					}
				}
				if name == "description" || property == "og:description" {
					if info.MetaDesc == "" {
						info.MetaDesc = content
						info.Zones.Meta = content
					}
				} else if name == "robots" {
					info.RobotsMeta = content
				}

			case atom.Link:
				var rel, href string
				for _, attr := range n.Attr {
					key := strings.ToLower(attr.Key)
					if key == "rel" {
						rel = strings.ToLower(attr.Val)
					} else if key == "href" {
						href = strings.TrimSpace(attr.Val)
					}
				}
				if strings.Contains(rel, "canonical") && info.Canonical == "" {
					info.Canonical = href
				}

			case atom.H1:
				h := extractText(n)
				if h != "" {
					info.H1 = append(info.H1, h)
					info.Zones.H1 = append(info.Zones.H1, h)
				}

			case atom.H2, atom.H3:
				h := extractText(n)
				if h != "" {
					info.H2H3 = append(info.H2H3, h)
					info.Zones.H2H3 = append(info.Zones.H2H3, h)
				}

			case atom.P, atom.Li, atom.Td, atom.Blockquote, atom.Figcaption:
				t := extractText(n)
				if t != "" {
					info.Zones.Body = append(info.Zones.Body, t)
					bodyTextBuilder.WriteString(t)
					bodyTextBuilder.WriteString(" ")
				}

			case atom.Img:
				hasAlt := false
				for _, attr := range n.Attr {
					if strings.EqualFold(attr.Key, "alt") {
						hasAlt = true
						val := strings.TrimSpace(attr.Val)
						if val != "" {
							info.Zones.Alt = append(info.Zones.Alt, val)
						}
					}
				}
				if !hasAlt {
					info.ImagesNoAlt++
				}

			case atom.A:
				for _, attr := range n.Attr {
					if strings.EqualFold(attr.Key, "href") {
						href := strings.TrimSpace(attr.Val)
						if href != "" && !strings.HasPrefix(href, "javascript:") && !strings.HasPrefix(href, "mailto:") && !strings.HasPrefix(href, "tel:") {
							links = append(links, href)
						}
					}
				}

			default:
				// Deteksi container SPA seperti id="root", id="__next", id="app"
				for _, attr := range n.Attr {
					if strings.EqualFold(attr.Key, "id") {
						idVal := strings.ToLower(attr.Val)
						if idVal == "root" || idVal == "__next" || idVal == "app" {
							hasSpaRoot = true
						}
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}

	traverse(doc)

	// Hitung WordCount
	words := strings.Fields(bodyTextBuilder.String())
	info.WordCount = len(words)

	// Deteksi ThinHTML (dugaan SPA / JS rendered)
	if info.WordCount < 50 && (scriptCount >= 3 || hasSpaRoot) {
		info.ThinHTML = true
	}

	// Slug URL untuk Zones.URL
	if u, err := url.Parse(pageURL); err == nil {
		slugTokens := strings.FieldsFunc(u.Path, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
		info.Zones.URL = strings.Join(slugTokens, " ")
	}

	return info, links, nil
}

func extractText(n *html.Node) string {
	var buf strings.Builder
	var collect func(*html.Node)
	collect = func(node *html.Node) {
		if node.Type == html.TextNode {
			buf.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && (c.DataAtom == atom.Script || c.DataAtom == atom.Style || c.DataAtom == atom.Noscript) {
				continue
			}
			collect(c)
		}
	}
	collect(n)
	return strings.Join(strings.Fields(buf.String()), " ")
}
