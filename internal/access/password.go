// Package access implements human authentication separately from device tokens.
package access

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

const passwordRounds = 600000
const legacySeedHash = "$pbkdf2-sha256$29000$UGptrTWGsPbeO2csJaTUWg$L0UlEAeIEwTtylnWYs2Jx/DgDdB174/k7ba7VeDj3xg"

// passlib uses unpadded adapted Base64, with '.' in place of '+'.
func ab64(b []byte) string {
	return strings.ReplaceAll(base64.RawStdEncoding.EncodeToString(b), "+", ".")
}
func decodeAB64(s string) ([]byte, error) {
	b, err := base64.RawStdEncoding.Strict().DecodeString(strings.ReplaceAll(s, ".", "+"))
	if err != nil || ab64(b) != s {
		return nil, errors.New("invalid hash encoding")
	}
	return b, nil
}
func checkPassword(encoded, password string) bool {
	if len(password) == 0 || len(password) > 1024 || !utf8.ValidString(password) || len(encoded) > 255 {
		return false
	}
	fields := strings.Split(encoded, "$")
	if len(fields) != 5 || fields[0] != "" || fields[1] != "pbkdf2-sha256" {
		return false
	}
	n, err := strconv.Atoi(fields[2])
	if err != nil || n < 1 || n > 2000000 || strconv.Itoa(n) != fields[2] {
		return false
	}
	salt, err := decodeAB64(fields[3])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false
	}
	want, err := decodeAB64(fields[4])
	if err != nil || len(want) != 32 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, n, 32)
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}
func hashPassword(password string) (string, error) {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len(password) > 1024 {
		return "", failure(422, "password_policy")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", unavailable()
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordRounds, 32)
	if err != nil {
		return "", unavailable()
	}
	return "$pbkdf2-sha256$" + strconv.Itoa(passwordRounds) + "$" + ab64(salt) + "$" + ab64(key), nil
}
func dummyPassword(password string) {
	// Bound the work even for unknown users; never disclose whether an ID exists.
	_, _ = pbkdf2.Key(sha256.New, password, []byte("timeweaver-dummy-salt"), passwordRounds, 32)
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", unavailable()
	}
	return hex.EncodeToString(b), nil
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func equalSecret(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
