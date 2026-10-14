package auth

import (
	"testing"
	"time"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("korrekt-pferd-batterie")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "korrekt-pferd-batterie") {
		t.Error("valid password rejected")
	}
	if CheckPassword(h, "falsch") {
		t.Error("invalid password accepted")
	}
	if CheckPassword("garbage", "x") {
		t.Error("garbage hash accepted")
	}
}

// RFC 6238 appendix B (SHA1), truncated to 6 digits.
func TestTOTPVectors(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" // "12345678901234567890"
	vectors := map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"}
	for ts, want := range vectors {
		got, err := TOTPCode(secret, time.Unix(ts, 0))
		if err != nil || got != want {
			t.Errorf("TOTP(%d) = %s, want %s", ts, got, want)
		}
	}
}

func TestTOTPReplay(t *testing.T) {
	secret := NewTOTPSecret()
	code, _ := TOTPCode(secret, time.Now())
	if !VerifyTOTP(secret, code, "u1") {
		t.Fatal("fresh code rejected")
	}
	if VerifyTOTP(secret, code, "u1") {
		t.Error("replayed code accepted")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("ip") {
			t.Fatal("blocked too early")
		}
	}
	if l.Allow("ip") {
		t.Error("limit not enforced")
	}
	if !l.Allow("other") {
		t.Error("keys not independent")
	}
}
