package analyze

import (
	"strings"
	"unicode"
)

var defaultStopwords = map[string]bool{
	// Bahasa Indonesia
	"yang": true, "di": true, "ke": true, "dari": true, "dan": true,
	"atau": true, "untuk": true, "dengan": true, "pada": true, "ini": true,
	"itu": true, "adalah": true, "cara": true,
	// English
	"the": true, "a": true, "an": true, "of": true, "to": true,
	"in": true, "for": true, "and": true, "or": true, "is": true, "how": true,
}

// Tokenize memecah teks menjadi token-token kata:
// 1. Lowercase
// 2. Pemisahan karakter non-huruf & non-angka
// 3. Membuang token 1 karakter
func Tokenize(text string) []string {
	lower := strings.ToLower(text)
	raw := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	var tokens []string
	for _, t := range raw {
		if len([]rune(t)) > 1 {
			tokens = append(tokens, t)
		}
	}
	return tokens
}

// NormalizePhrase mengubah string frasa menjadi kata-kata ternormalisasi yang digabung spasi tunggal.
func NormalizePhrase(phrase string) string {
	toks := Tokenize(phrase)
	return strings.Join(toks, " ")
}

// IsStopword memeriksa apakah suatu token adalah stopword (bawaan atau kustom).
func IsStopword(token string, customStopwords []string) bool {
	lower := strings.ToLower(token)
	if defaultStopwords[lower] {
		return true
	}
	for _, cs := range customStopwords {
		if strings.EqualFold(cs, lower) {
			return true
		}
	}
	return false
}

// FilterNonStopwords mengembalikan daftar token yang bukan stopword.
func FilterNonStopwords(tokens []string, customStopwords []string) []string {
	var result []string
	for _, t := range tokens {
		if !IsStopword(t, customStopwords) {
			result = append(result, t)
		}
	}
	return result
}

