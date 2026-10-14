package mapr

import (
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRenderFixtures renders real region files if HEARTH_MCA_FIXTURES is set.
func TestRenderFixtures(t *testing.T) {
	dir := os.Getenv("HEARTH_MCA_FIXTURES")
	out := os.Getenv("HEARTH_MCA_OUT")
	if dir == "" {
		t.Skip("no fixtures")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*", "*.mca"))
	more, _ := filepath.Glob(filepath.Join(dir, "*.mca"))
	files = append(files, more...)
	for _, f := range files {
		name := filepath.Base(filepath.Dir(f)) + "_" + filepath.Base(f)
		start := time.Now()
		// rename-free: OpenRegion requires r.X.Z.mca naming
		reg := f
		if _, _, ok := ParseRegionName(filepath.Base(f)); !ok {
			tmp := filepath.Join(t.TempDir(), "r.0.0.mca")
			b, _ := os.ReadFile(f)
			os.WriteFile(tmp, b, 0o644)
			reg = tmp
		}
		img, chunks, err := RenderRegion(reg, RenderOptions{})
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		// count opaque pixels
		opaque := 0
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] > 0 {
				opaque++
			}
		}
		t.Logf("%-28s chunks=%4d opaque=%6d in %v", name, chunks, opaque, time.Since(start).Round(time.Millisecond))
		if chunks == 0 {
			t.Errorf("%s: no chunks rendered", name)
		}
		if out != "" {
			bg := image.NewNRGBA(img.Bounds())
			draw.Draw(bg, bg.Bounds(), &image.Uniform{color.NRGBA{40, 40, 48, 255}}, image.Point{}, draw.Src)
			draw.Draw(bg, bg.Bounds(), img, image.Point{}, draw.Over)
			writePNG(filepath.Join(out, name+".png"), bg)
		}
	}
}
