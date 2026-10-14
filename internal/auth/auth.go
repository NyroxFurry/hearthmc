// Package auth implements password hashing, TOTP two-factor authentication,
// random tokens and simple rate limiting.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// ---- passwords (argon2id, PHC string format) ----

const (
	argonTime    = 2
	argonMemory  = 19 * 1024 // KiB (OWASP recommendation)
	argonThreads = 1
	argonKeyLen  = 32
)

func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

var ErrBadHash = errors.New("invalid password hash")

func CheckPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is used to keep login timing constant for unknown users.
var dummyHash, _ = HashPassword("hearth-dummy-password")

func CheckDummy(pw string) { CheckPassword(dummyHash, pw) }

// ValidatePassword enforces a minimal password policy.
func ValidatePassword(pw string) error {
	if len([]rune(pw)) < 8 {
		return errors.New("Das Passwort muss mindestens 8 Zeichen lang sein")
	}
	if len(pw) > 256 {
		return errors.New("Das Passwort ist zu lang")
	}
	return nil
}

// ---- tokens ----

// RandomToken returns a URL-safe random token with nBytes of entropy.
func RandomToken(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken returns the hex SHA-256 of a token (what we store in the database).
func HashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// ---- TOTP (RFC 6238) ----

func NewTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

func TOTPURL(issuer, account, secret string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + v.Encode()
}

func hotp(key []byte, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%06d", code%1_000_000)
}

// TOTPCode returns the code for time t (exported for tests).
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if err != nil {
		return "", err
	}
	return hotp(key, uint64(t.Unix()/30)), nil
}

// usedCodes prevents replay of a TOTP code within its validity window.
var usedCodes = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

// VerifyTOTP checks a 6-digit code allowing ±1 time step of clock drift.
// replayKey (e.g. the user id) is used to reject reuse of the same code.
func VerifyTOTP(secret, code, replayKey string) bool {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return false
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if err != nil {
		return false
	}
	now := time.Now()
	step := now.Unix() / 30
	for _, d := range []int64{0, -1, 1} {
		c := hotp(key, uint64(step+d))
		if subtle.ConstantTimeCompare([]byte(c), []byte(code)) == 1 {
			k := replayKey + ":" + code
			usedCodes.Lock()
			defer usedCodes.Unlock()
			for kk, exp := range usedCodes.m {
				if now.After(exp) {
					delete(usedCodes.m, kk)
				}
			}
			if _, used := usedCodes.m[k]; used {
				return false
			}
			usedCodes.m[k] = now.Add(2 * time.Minute)
			return true
		}
	}
	return false
}

// NewRecoveryCodes returns n human friendly codes plus their hashes.
func NewRecoveryCodes(n int) (codes, hashes []string) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	for range n {
		b := make([]byte, 10)
		rand.Read(b)
		var sb strings.Builder
		for i, c := range b {
			if i == 5 {
				sb.WriteByte('-')
			}
			sb.WriteByte(alphabet[int(c)%len(alphabet)])
		}
		code := sb.String()
		codes = append(codes, code)
		hashes = append(hashes, HashToken(code))
	}
	return
}

// ---- rate limiting ----

// Limiter is a simple fixed-window limiter keyed by string.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*window
}

type window struct {
	start time.Time
	n     int
}

func NewLimiter(max int, win time.Duration) *Limiter {
	l := &Limiter{max: max, window: win, hits: map[string]*window{}}
	go func() {
		for range time.Tick(win) {
			l.mu.Lock()
			now := time.Now()
			for k, w := range l.hits {
				if now.Sub(w.start) > l.window {
					delete(l.hits, k)
				}
			}
			l.mu.Unlock()
		}
	}()
	return l
}

// Allow records a hit and reports whether it is within the limit.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	w, ok := l.hits[key]
	if !ok || now.Sub(w.start) > l.window {
		l.hits[key] = &window{start: now, n: 1}
		return true
	}
	w.n++
	return w.n <= l.max
}

// Blocked reports whether key is currently over the limit without recording a hit.
func (l *Limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.hits[key]
	return ok && time.Since(w.start) <= l.window && w.n >= l.max
}

func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.hits, key)
	l.mu.Unlock()
}
