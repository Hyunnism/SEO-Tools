package analyze

import (
	"math"
	"sort"
	"strings"

	"seo-boost/internal/model"
)

type zoneEntry struct {
	name   string
	tokens []string
	phrase string
}

// ExpectedCTR mengembalikan ekspektasi CTR berbasis posisi pencarian di Google (Tabel 9.6).
func ExpectedCTR(pos float64) float64 {
	switch {
	case pos <= 1.0:
		return 0.28
	case pos <= 2.0:
		return 0.15
	case pos <= 3.0:
		return 0.11
	case pos <= 4.0:
		return 0.08
	case pos <= 5.0:
		return 0.07
	case pos <= 6.0:
		return 0.05
	case pos <= 7.0:
		return 0.04
	case pos <= 8.0:
		return 0.035
	case pos <= 9.0:
		return 0.03
	case pos <= 10.0:
		return 0.025
	case pos <= 20.0:
		return 0.01
	default:
		return 0.003
	}
}

// MatchWeights mendefinisikan bobot pengali potensi klik berdasarkan tingkat kecocokan teks.
var MatchWeights = map[model.MatchLevel]float64{
	model.MatchAbsent:  1.0,
	model.MatchPartial: 0.9,
	model.MatchAllWord: 0.6,
	model.MatchExact:   0.3,
}

// CalculateScore menghitung opportunity score:
// targetPos = max(1, pos - 3)
// potensiKlik = impressions * max(0, expectedCTR(targetPos) - ctr)
// score = potensiKlik * bobotMatch
func CalculateScore(impressions, ctr, position float64, match model.MatchLevel) float64 {
	targetPos := math.Max(1.0, position-3.0)
	expCTR := ExpectedCTR(targetPos)
	potensiKlik := impressions * math.Max(0.0, expCTR-ctr)
	bobot := MatchWeights[match]
	if bobot == 0 {
		bobot = 1.0
	}
	return potensiKlik * bobot
}

// DetermineCategory mengklasifikasikan kueri ke dalam badge teks informatif.
func DetermineCategory(pos, impressions, ctr float64, match model.MatchLevel, minImpressions int) model.GapCategory {
	if minImpressions <= 0 {
		minImpressions = 50
	}
	minImp := float64(minImpressions)

	// 1. Sudah kuat (posisi <= 10 dan exact match)
	if pos <= 10.0 && match == model.MatchExact {
		return model.CategoryAlreadyStrong // "STRONG"
	}

	// 2. Title/Meta fix (posisi <= 10, CTR < 50% dari expectedCTR)
	if pos <= 10.0 && ctr < ExpectedCTR(pos)*0.5 {
		return model.CategoryTitleMetaFix // "TITLE_META"
	}

	// 3. Konten belum menjawab (absent, impresi cukup)
	if match == model.MatchAbsent && impressions >= minImp {
		return model.CategoryContentMissing // "NEW_CONTENT"
	}

	// 4. Low-hanging fruit / Boost (posisi 5-20, impresi cukup, kata sudah ada tapi belum exact)
	if pos >= 5.0 && pos <= 20.0 && impressions >= minImp && (match == model.MatchPartial || match == model.MatchAllWord) {
		return model.CategoryLowHangingFruit // "BOOST"
	}

	return ""
}

// AnalyzeKeywordGap menganalisis kesenjangan kata kunci untuk 1 query terhadap 1 halaman.
func AnalyzeKeywordGap(queryPerf model.QueryPerf, page *model.Page, customStopwords []string, minImpressions int) model.QueryGap {
	gap := model.QueryGap{
		QueryPerf: queryPerf,
	}

	queryTokens := Tokenize(queryPerf.Query)
	if len(queryTokens) == 0 {
		gap.Match = model.MatchAbsent
		gap.Score = 0
		return gap
	}

	queryNorm := strings.Join(queryTokens, " ")
	nonStopwords := FilterNonStopwords(queryTokens, customStopwords)

	zones := extractZones(page)

	// 1. Cek Exact Match pada setiap zona (urutan hierarki zona)
	isExact := false
	var foundIn []string
	zoneTokensMap := make(map[string]bool)

	for _, z := range zones {
		containsExact := strings.Contains(" "+z.phrase+" ", " "+queryNorm+" ")
		if containsExact {
			isExact = true
			foundIn = append(foundIn, z.name)
		}
		for _, tok := range z.tokens {
			zoneTokensMap[tok] = true
		}
	}

	if isExact {
		gap.Match = model.MatchExact
		gap.FoundIn = foundIn
		gap.Score = CalculateScore(queryPerf.Impressions, queryPerf.CTR, queryPerf.Position, model.MatchExact)
		gap.Category = DetermineCategory(queryPerf.Position, queryPerf.Impressions, queryPerf.CTR, model.MatchExact, minImpressions)
		return gap
	}

	// 2. Jika bukan exact, cek kecocokan kata-kata non-stopword
	if len(nonStopwords) == 0 {
		// Jika query hanya terdiri dari stopwords dan bukan exact match
		gap.Match = model.MatchAbsent
		gap.Score = 0
		return gap
	}

	var missingTokens []string
	presentCount := 0

	for _, nsw := range nonStopwords {
		if zoneTokensMap[nsw] {
			presentCount++
		} else {
			missingTokens = append(missingTokens, nsw)
		}
	}

	// Tentukan zona mana saja yang memuat token query
	for _, z := range zones {
		hasAny := false
		for _, tok := range z.tokens {
			for _, nsw := range nonStopwords {
				if tok == nsw {
					hasAny = true
					break
				}
			}
			if hasAny {
				break
			}
		}
		if hasAny {
			foundIn = append(foundIn, z.name)
		}
	}
	gap.FoundIn = foundIn
	gap.MissingTok = missingTokens

	if presentCount == len(nonStopwords) {
		gap.Match = model.MatchAllWord
	} else if presentCount > 0 {
		gap.Match = model.MatchPartial
	} else {
		gap.Match = model.MatchAbsent
	}

	gap.Score = CalculateScore(queryPerf.Impressions, queryPerf.CTR, queryPerf.Position, gap.Match)
	gap.Category = DetermineCategory(queryPerf.Position, queryPerf.Impressions, queryPerf.CTR, gap.Match, minImpressions)
	return gap
}

// AnalyzePageGaps mengevaluasi seluruh query pada halaman dan mengurutkannya dari skor peluang terbesar.
func AnalyzePageGaps(page *model.Page, customStopwords []string, minImpressions int) []model.QueryGap {
	if page == nil || page.Perf == nil || len(page.Perf.Queries) == 0 {
		return nil
	}

	var gaps []model.QueryGap
	for _, q := range page.Perf.Queries {
		g := AnalyzeKeywordGap(q, page, customStopwords, minImpressions)
		gaps = append(gaps, g)
	}

	sort.SliceStable(gaps, func(i, j int) bool {
		return gaps[i].Score > gaps[j].Score
	})

	return gaps
}

func extractZones(page *model.Page) []zoneEntry {
	if page == nil || page.Crawl == nil {
		return nil
	}
	c := page.Crawl

	// Urutan hierarki: title > h1 > h2h3 > meta > url > body > alt
	var entries []zoneEntry

	// 1. Title
	tText := c.Title
	if c.Zones.Title != "" {
		tText = c.Zones.Title
	}
	entries = append(entries, makeZone("title", tText))

	// 2. H1
	h1Text := strings.Join(c.H1, " ")
	if len(c.Zones.H1) > 0 {
		h1Text = strings.Join(c.Zones.H1, " ")
	}
	entries = append(entries, makeZone("h1", h1Text))

	// 3. H2H3
	h2h3Text := strings.Join(c.H2H3, " ")
	if len(c.Zones.H2H3) > 0 {
		h2h3Text = strings.Join(c.Zones.H2H3, " ")
	}
	entries = append(entries, makeZone("h2h3", h2h3Text))

	// 4. Meta Description
	mText := c.MetaDesc
	if c.Zones.Meta != "" {
		mText = c.Zones.Meta
	}
	entries = append(entries, makeZone("meta", mText))

	// 5. URL
	uText := page.URL
	if c.Zones.URL != "" {
		uText = c.Zones.URL
	}
	entries = append(entries, makeZone("url", uText))

	// 6. Body
	bText := strings.Join(c.Zones.Body, " ")
	entries = append(entries, makeZone("body", bText))

	// 7. Alt
	altText := strings.Join(c.Zones.Alt, " ")
	entries = append(entries, makeZone("alt", altText))

	return entries
}

func makeZone(name, text string) zoneEntry {
	toks := Tokenize(text)
	return zoneEntry{
		name:   name,
		tokens: toks,
		phrase: strings.Join(toks, " "),
	}
}

