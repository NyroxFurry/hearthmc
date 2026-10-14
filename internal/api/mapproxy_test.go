package api

import (
	"strings"
	"testing"
)

func TestMapToken(t *testing.T) {
	tok := mapToken("abc12345")
	if !validMapToken("abc12345", tok) {
		t.Fatal("fresh token rejected")
	}
	if validMapToken("other123", tok) {
		t.Error("token valid for another server")
	}
	if validMapToken("abc12345", strings.ToUpper(tok)) || validMapToken("abc12345", "x"+tok) || validMapToken("abc12345", "") {
		t.Error("tampered token accepted")
	}
}

// flavorFor must pick the same colors as web/src/lib/flavors.ts.
func TestFlavorForMatchesFrontend(t *testing.T) {
	// indexes computed with the frontend hash (node) for these ids
	for id, idx := range map[string]int{"pbwn79nv": 4, "bmbtoccv": 1, "c9drrpbu": 3, "a1b2c3d4": 5, "zzzzzzzz": 4} {
		if f := flavorFor("", id); f != flavors[idx] {
			t.Errorf("%s: got %+v, want %+v", id, f, flavors[idx])
		}
	}
	if f := flavorFor("#4fae94", ""); f.Color != "#3fd6a4" {
		t.Errorf("legacy color not mapped: %+v", f)
	}
	if f := flavorFor("#E0457B", ""); f.BG != "#3e1024" {
		t.Errorf("case-insensitive match failed: %+v", f)
	}
	if f := flavorFor("#808080", ""); f.Color != "#808080" || f.BG != "#212121" {
		t.Errorf("custom color: %+v", f)
	}
}
