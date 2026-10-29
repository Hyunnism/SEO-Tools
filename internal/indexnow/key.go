package indexnow

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
)

var hexRegex = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// GenerateKey menghasilkan kunci IndexNow acak 32-karakter hexadecimal (128-bit entropy).
func GenerateKey() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("gagal membaca crypto/rand: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// ValidateKeyFormat memvalidasi format kunci IndexNow (panjang 8-128 karakter hexadecimal).
func ValidateKeyFormat(key string) error {
	length := len(key)
	if length < 8 || length > 128 {
		return fmt.Errorf("panjang kunci IndexNow harus antara 8 dan 128 karakter, didapat: %d", length)
	}
	if !hexRegex.MatchString(key) {
		return fmt.Errorf("kunci IndexNow hanya boleh memuat karakter heksadesimal (0-9, a-f)")
	}
	return nil
}

