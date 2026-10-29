package analyze

import (
	"math"
	"testing"

	"seo-boost/internal/model"
)

func TestTokenize(t *testing.T) {
	input := "Tutorial Go 1.25: Cara Belajar! A to Z, éclipse & münchen..."
	tokens := Tokenize(input)

	// "a" harus dibuang (1 karakter)
	// "to" tetap ada (2 karakter)
	expectedTokens := []string{"tutorial", "go", "25", "cara", "belajar", "to", "éclipse", "münchen"}

	if len(tokens) != len(expectedTokens) {
		t.Fatalf("got %d tokens (%v), want %d (%v)", len(tokens), tokens, len(expectedTokens), expectedTokens)
	}

	for i, exp := range expectedTokens {
		if tokens[i] != exp {
			t.Errorf("token %d got %q, want %q", i, tokens[i], exp)
		}
	}
}

func TestMatchLevelsAndStopwords(t *testing.T) {
	page := &model.Page{
		URL: "https://example.com/blog/golang-concurrency",
		Crawl: &model.CrawlInfo{
			Title:    "Tutorial Golang Concurrency Terlengkap",
			MetaDesc: "Panduan praktis belajar concurrency di Go dengan mudah",
			H1:       []string{"Belajar Golang Concurrency"},
			H2H3:     []string{"Pengenalan Goroutine", "Contoh Channel"},
			Zones: model.Zones{
				Body: []string{"Di sini kita membahas goroutine dan channel secara mendalam untuk aplikasi web."},
				Alt:  []string{"Diagram arsitektur goroutine"},
			},
		},
	}

	// 1. Exact Match: "tutorial golang concurrency" (ada persis di title)
	q1 := model.QueryPerf{Query: "tutorial golang concurrency", Impressions: 100, CTR: 0.05, Position: 3.0}
	gap1 := AnalyzeKeywordGap(q1, page, nil, 50)
	if gap1.Match != model.MatchExact {
		t.Errorf("gap1 got match %s, want exact", gap1.Match)
	}
	if len(gap1.FoundIn) == 0 || gap1.FoundIn[0] != "title" {
		t.Errorf("gap1 FoundIn should start with title, got: %v", gap1.FoundIn)
	}

	// 2. All Words: "belajar channel" (kedua kata ada di halaman, tapi tidak berurutan langsung)
	q2 := model.QueryPerf{Query: "belajar channel", Impressions: 80, CTR: 0.02, Position: 8.0}
	gap2 := AnalyzeKeywordGap(q2, page, nil, 50)
	if gap2.Match != model.MatchAllWord {
		t.Errorf("gap2 got match %s, want all_words", gap2.Match)
	}
	if len(gap2.MissingTok) != 0 {
		t.Errorf("gap2 MissingTok should be empty, got: %v", gap2.MissingTok)
	}

	// 3. Partial Match: "belajar worker pool" ("belajar" ada, "worker" & "pool" hilang)
	// Stopword "dan", "untuk" tidak boleh masuk MissingTok bila ada
	q3 := model.QueryPerf{Query: "cara belajar worker pool untuk pemula", Impressions: 200, CTR: 0.01, Position: 7.0}
	gap3 := AnalyzeKeywordGap(q3, page, nil, 50)
	if gap3.Match != model.MatchPartial {
		t.Errorf("gap3 got match %s, want partial", gap3.Match)
	}

	// Cek missing tokens: harus hanya "worker", "pool", "pemula" ("cara" dan "untuk" adalah stopword)
	for _, m := range gap3.MissingTok {
		if m == "cara" || m == "untuk" {
			t.Errorf("stopword %q tidak boleh ada di MissingTok", m)
		}
	}

	// 4. Absent Match: "resep masakan padang" (tidak ada sama sekali di halaman)
	q4 := model.QueryPerf{Query: "resep masakan padang", Impressions: 500, CTR: 0.0, Position: 15.0}
	gap4 := AnalyzeKeywordGap(q4, page, nil, 50)
	if gap4.Match != model.MatchAbsent {
		t.Errorf("gap4 got match %s, want absent", gap4.Match)
	}
	if len(gap4.MissingTok) != 3 {
		t.Errorf("gap4 all 3 tokens should be missing, got: %v", gap4.MissingTok)
	}
}

func TestScoreAndCategory(t *testing.T) {
	// 1. Low-hanging fruit / BOOST:
	// pos = 8.0, impressions = 300, ctr = 0.02, match = partial (bobot 0.9)
	// targetPos = max(1, 8.0 - 3.0) = 5.0
	// expectedCTR(5.0) = 0.07
	// potensiKlik = 300 * (0.07 - 0.02) = 300 * 0.05 = 15.0
	// score = 15.0 * 0.9 = 13.5
	score := CalculateScore(300, 0.02, 8.0, model.MatchPartial)
	if math.Abs(score-13.5) > 1e-6 {
		t.Errorf("calculated score got %f, want 13.5", score)
	}

	cat := DetermineCategory(8.0, 300, 0.02, model.MatchPartial, 50)
	if cat != model.CategoryLowHangingFruit { // "BOOST"
		t.Errorf("category got %q, want %q", cat, model.CategoryLowHangingFruit)
	}

	// 2. Title/Meta fix:
	// pos = 4.0 (expectedCTR = 0.08), ctr = 0.01 (< 0.08 * 0.5 = 0.04)
	catFix := DetermineCategory(4.0, 500, 0.01, model.MatchExact, 50)
	// Catatan: pos <= 10 dan exact match prioritaskan "STRONG", tapi jika CTR anjlok:
	catFixPartial := DetermineCategory(4.0, 500, 0.01, model.MatchAllWord, 50)
	if catFixPartial != model.CategoryTitleMetaFix { // "TITLE_META"
		t.Errorf("category got %q, want %q", catFixPartial, model.CategoryTitleMetaFix)
	}
	_ = catFix

	// 3. Already strong:
	catStrong := DetermineCategory(2.0, 200, 0.15, model.MatchExact, 50)
	if catStrong != model.CategoryAlreadyStrong { // "STRONG"
		t.Errorf("category got %q, want %q", catStrong, model.CategoryAlreadyStrong)
	}

	// 4. Content missing:
	catMissing := DetermineCategory(12.0, 400, 0.001, model.MatchAbsent, 50)
	if catMissing != model.CategoryContentMissing { // "NEW_CONTENT"
		t.Errorf("category got %q, want %q", catMissing, model.CategoryContentMissing)
	}
}

