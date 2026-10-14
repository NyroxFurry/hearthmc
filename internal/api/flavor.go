package api

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Farbsorten wie in web/src/lib/flavors.ts: kräftige Farbe und tiefer Grund.
// Muss mit der Liste im Frontend übereinstimmen.
type flavor struct {
	Color string
	BG    string
}

var flavors = []flavor{
	{"#3fd6a4", "#0e3b30"}, // Minze
	{"#e0457b", "#3e1024"}, // Beere
	{"#3aa7f0", "#0d2a47"}, // Ozean
	{"#f7b32b", "#3d2a06"}, // Zitrus
	{"#a98bf0", "#251a45"}, // Lavendel
	{"#ff8a4c", "#40190a"}, // Orange
	{"#a6d83a", "#26330b"}, // Limette
	{"#dce3e8", "#1e2428"}, // Neutral
}

var legacyFlavor = map[string]int{
	"#4fae94": 0, "#d16a8f": 1, "#e5584f": 1, "#5a8fd6": 2, "#b7a03c": 3,
	"#9d88e6": 4, "#e08a4c": 5, "#c4734f": 5, "#5fa95a": 6,
}

// flavorFor mirrors flavorFor() in the frontend: a stored color, or a stable
// pick from the server ID when no color was chosen.
func flavorFor(color, seed string) flavor {
	c := strings.ToLower(strings.TrimSpace(color))
	if c == "" {
		if seed == "" {
			return flavors[0]
		}
		var h uint32
		for _, r := range seed {
			h = h*31 + uint32(r)
		}
		return flavors[h%uint32(len(flavors)-1)]
	}
	for _, f := range flavors {
		if f.Color == c {
			return f
		}
	}
	if i, ok := legacyFlavor[c]; ok {
		return flavors[i]
	}
	if len(c) == 7 && c[0] == '#' {
		if v, err := strconv.ParseUint(c[1:], 16, 32); err == nil {
			r, g, b := float64(v>>16&0xff), float64(v>>8&0xff), float64(v&0xff)
			return flavor{Color: c, BG: fmt.Sprintf("#%02x%02x%02x", int(math.Round(r*0.26)), int(math.Round(g*0.26)), int(math.Round(b*0.26)))}
		}
	}
	return flavors[0]
}
