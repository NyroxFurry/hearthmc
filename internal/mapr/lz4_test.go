package mapr

import (
	"bytes"
	"os"
	"testing"
)

// testdata was produced with the reference LZ4 implementation (python-lz4)
// framed like lz4-java's LZ4BlockOutputStream, as Minecraft writes it.
func TestLZ4BlockStream(t *testing.T) {
	in, err := os.ReadFile("testdata/lz4test.bin")
	if err != nil {
		t.Skip(err)
	}
	want, _ := os.ReadFile("testdata/lz4test.raw")
	got, err := lz4BlockStream(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded %d bytes, want %d (mismatch)", len(got), len(want))
	}
}

func TestBitsFor(t *testing.T) {
	for n, want := range map[int]uint{1: 0, 2: 1, 3: 2, 4: 2, 5: 3, 16: 4, 17: 5, 256: 8} {
		if got := bitsFor(n); got != want {
			t.Errorf("bitsFor(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestBlockColors(t *testing.T) {
	if !Info("minecraft:air").Invisible || !Info("minecraft:torch").Invisible {
		t.Error("air/torch should be invisible")
	}
	if !Info("minecraft:water").Water || !Info("minecraft:kelp").Water {
		t.Error("water plants")
	}
	if Info("minecraft:oak_leaves").Tint != TintFoliage || Info("minecraft:grass_block").Tint != TintGrass {
		t.Error("tints")
	}
	if c := Info("minecraft:red_wool").Color; c != hexRGB(0xa12722) {
		t.Errorf("red_wool = %v", c)
	}
	if c := Info("minecraft:spruce_stairs").Color; c != hexRGB(woodColors["spruce"]) {
		t.Errorf("spruce_stairs = %v", c)
	}
	if c := Info("minecraft:waxed_weathered_cut_copper_slab").Color; c == (RGB{}) {
		t.Error("copper variant has no color")
	}
}
