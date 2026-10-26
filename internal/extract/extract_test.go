package extract

import (
	"strings"
	"testing"
)

func TestExtractHTML_Standard(t *testing.T) {
	htmlContent := `<!DOCTYPE html>
<html lang="id">
<head>
  <meta charset="utf-8">
  <title>Panduan Lengkap Belajar Golang untuk Pemula</title>
  <meta name="description" content="Tutorial belajar bahasa pemrograman Go dari dasar hingga mahir.">
  <meta name="robots" content="index, follow">
  <link rel="canonical" href="https://example.com/blog/belajar-golang">
  <script type="application/ld+json">
    {"@context":"https://schema.org","@type":"Article","headline":"Belajar Go"}
  </script>
  <style>
    body { color: red; }
    h1 { font-size: 20px; }
  </style>
</head>
<body>
  <h1>Belajar Golang Dasar</h1>
  <h2>Instalasi Go</h2>
  <p>Golang adalah bahasa pemrograman yang cepat dan efisien.</p>
  <h3>Fitur Concurrency</h3>
  <p>Goroutine memudahkan eksekusi fungsi secara bersamaan di background.</p>
  <img src="/img/logo.png" alt="Logo Go">
  <img src="/img/chart.png"> <!-- no alt -->
  <a href="/blog/tips-docker">Tips Docker</a>
  <a href="https://google.com">Google</a>
  <script>
    console.log("script code here should be ignored");
  </script>
</body>
</html>`

	info, links, err := ExtractHTML(strings.NewReader(htmlContent), "https://example.com/blog/belajar-golang")
	if err != nil {
		t.Fatalf("ExtractHTML failed: %v", err)
	}

	if info.Title != "Panduan Lengkap Belajar Golang untuk Pemula" {
		t.Errorf("Title = %q", info.Title)
	}
	if info.MetaDesc != "Tutorial belajar bahasa pemrograman Go dari dasar hingga mahir." {
		t.Errorf("MetaDesc = %q", info.MetaDesc)
	}
	if info.Canonical != "https://example.com/blog/belajar-golang" {
		t.Errorf("Canonical = %q", info.Canonical)
	}
	if info.RobotsMeta != "index, follow" {
		t.Errorf("RobotsMeta = %q", info.RobotsMeta)
	}
	if !info.HasJSONLD {
		t.Error("HasJSONLD harus true")
	}
	if len(info.H1) != 1 || info.H1[0] != "Belajar Golang Dasar" {
		t.Errorf("H1 = %v", info.H1)
	}
	if len(info.H2H3) != 2 || info.H2H3[0] != "Instalasi Go" || info.H2H3[1] != "Fitur Concurrency" {
		t.Errorf("H2H3 = %v", info.H2H3)
	}
	if info.ImagesNoAlt != 1 {
		t.Errorf("ImagesNoAlt = %d, want 1", info.ImagesNoAlt)
	}
	if len(info.Zones.Alt) != 1 || info.Zones.Alt[0] != "Logo Go" {
		t.Errorf("Zones.Alt = %v", info.Zones.Alt)
	}
	if len(links) != 2 {
		t.Errorf("links = %v, want 2 links", links)
	}
	if info.WordCount < 10 {
		t.Errorf("WordCount = %d, terlalu sedikit", info.WordCount)
	}
	if info.ThinHTML {
		t.Error("ThinHTML harus false untuk halaman ini")
	}
	if info.Zones.URL != "blog belajar golang" {
		t.Errorf("Zones.URL = %q, want 'blog belajar golang'", info.Zones.URL)
	}
}

func TestExtractHTML_Malformed(t *testing.T) {
	brokenHTML := `<html>
<head>
  <title>Judul Test</title>
  <meta name="description"
        content="Deskripsi multiline">
<body>
  <h1>Header Satu
  <p>Paragraf tidak tertutup
  <a href="/link1">Link Satu
  <a href="/link2">Link Dua`

	info, links, err := ExtractHTML(strings.NewReader(brokenHTML), "https://example.com/test")
	if err != nil {
		t.Fatalf("ExtractHTML gagal mengurai HTML rusak: %v", err)
	}

	if info.Title != "Judul Test" {
		t.Errorf("Title = %q", info.Title)
	}
	if info.MetaDesc != "Deskripsi multiline" {
		t.Errorf("MetaDesc = %q", info.MetaDesc)
	}
	if len(info.H1) == 0 || !strings.Contains(info.H1[0], "Header Satu") {
		t.Errorf("H1 = %v", info.H1)
	}
	if len(links) != 2 {
		t.Errorf("links = %v", links)
	}
}

func TestExtractHTML_ThinHTML_SPA(t *testing.T) {
	spaHTML := `<!DOCTYPE html>
<html>
<head><title>Aplikasi React</title></head>
<body>
  <div id="root"></div>
  <script src="/static/js/bundle1.js"></script>
  <script src="/static/js/bundle2.js"></script>
  <script src="/static/js/bundle3.js"></script>
</body>
</html>`

	info, _, err := ExtractHTML(strings.NewReader(spaHTML), "https://example.com/app")
	if err != nil {
		t.Fatalf("ExtractHTML failed: %v", err)
	}

	if !info.ThinHTML {
		t.Error("ThinHTML harus true untuk container SPA dengan sedikit kata")
	}
}
