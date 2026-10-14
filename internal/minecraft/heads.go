package minecraft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hearth/internal/httpx"
)

// Heads renders player head images from Minecraft skins (cached on disk).
type Heads struct {
	HTTP *httpx.Client
	Dir  string

	mu       sync.Mutex
	inflight map[string]chan struct{}
	failed   map[string]time.Time
}

const headTTL = 12 * time.Hour

// PNG returns a PNG of the player's face (with hat layer) scaled to size px.
func (h *Heads) PNG(ctx context.Context, uuid string, size int) ([]byte, error) {
	if size < 8 {
		size = 8
	}
	if size > 512 {
		size = 512
	}
	face := h.face(ctx, uuid)
	return scalePNG(face, size)
}

func (h *Heads) cachePath(uuid string) string {
	return filepath.Join(h.Dir, strings.ReplaceAll(uuid, "-", "")+".png")
}

func (h *Heads) face(ctx context.Context, uuid string) image.Image {
	u := NormalizeUUID(uuid)
	if u == "" {
		return DefaultFace(uuid)
	}
	p := h.cachePath(u)
	if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) < headTTL {
		if img := loadPNG(p); img != nil {
			return img
		}
	}
	// offline-mode UUIDs (version 3) have no skin at Mojang
	if u[14] == '3' {
		return DefaultFace(u)
	}
	h.mu.Lock()
	if h.inflight == nil {
		h.inflight = map[string]chan struct{}{}
		h.failed = map[string]time.Time{}
	}
	if t, ok := h.failed[u]; ok && time.Since(t) < 10*time.Minute {
		h.mu.Unlock()
		if img := loadPNG(p); img != nil {
			return img
		}
		return DefaultFace(u)
	}
	if ch, ok := h.inflight[u]; ok {
		h.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
		}
		if img := loadPNG(p); img != nil {
			return img
		}
		return DefaultFace(u)
	}
	ch := make(chan struct{})
	h.inflight[u] = ch
	h.mu.Unlock()

	img, err := h.fetch(ctx, u)

	h.mu.Lock()
	delete(h.inflight, u)
	if err != nil {
		h.failed[u] = time.Now()
	}
	h.mu.Unlock()
	close(ch)
	if err != nil {
		if old := loadPNG(p); old != nil {
			return old
		}
		return DefaultFace(u)
	}
	os.MkdirAll(h.Dir, 0o755)
	var buf bytes.Buffer
	png.Encode(&buf, img)
	os.WriteFile(p, buf.Bytes(), 0o644)
	return img
}

func (h *Heads) fetch(ctx context.Context, uuid string) (image.Image, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var prof struct {
		Properties []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"properties"`
	}
	if err := h.HTTP.GetJSON(ctx, "https://sessionserver.mojang.com/session/minecraft/profile/"+strings.ReplaceAll(uuid, "-", ""), 0, &prof); err != nil {
		return nil, err
	}
	skinURL := ""
	for _, p := range prof.Properties {
		if p.Name != "textures" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(p.Value)
		if err != nil {
			continue
		}
		var tex struct {
			Textures struct {
				Skin struct {
					URL string `json:"url"`
				} `json:"SKIN"`
			} `json:"textures"`
		}
		if json.Unmarshal(raw, &tex) == nil {
			skinURL = tex.Textures.Skin.URL
		}
	}
	if skinURL == "" {
		return DefaultFace(uuid), nil
	}
	skinURL = strings.Replace(skinURL, "http://", "https://", 1)
	b, err := h.HTTP.Get(ctx, skinURL, 0)
	if err != nil {
		return nil, err
	}
	skin, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return extractFace(skin)
}

func extractFace(skin image.Image) (image.Image, error) {
	bnd := skin.Bounds()
	if bnd.Dx() < 64 || bnd.Dy() < 32 {
		return nil, errors.New("ungültiger Skin")
	}
	scale := bnd.Dx() / 64 // HD skins
	face := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			base := skin.At(bnd.Min.X+(8+x)*scale, bnd.Min.Y+(8+y)*scale)
			r, g, b, _ := base.RGBA()
			face.Set(x, y, color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 255})
			hat := color.NRGBAModel.Convert(skin.At(bnd.Min.X+(40+x)*scale, bnd.Min.Y+(8+y)*scale)).(color.NRGBA)
			if hat.A > 0 {
				cur := face.NRGBAAt(x, y)
				a := float64(hat.A) / 255
				face.SetNRGBA(x, y, color.NRGBA{
					uint8(float64(hat.R)*a + float64(cur.R)*(1-a)),
					uint8(float64(hat.G)*a + float64(cur.G)*(1-a)),
					uint8(float64(hat.B)*a + float64(cur.B)*(1-a)), 255})
			}
		}
	}
	return face, nil
}

func loadPNG(p string) image.Image {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

func scalePNG(src image.Image, size int) ([]byte, error) {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	sb := src.Bounds()
	for y := 0; y < size; y++ {
		sy := sb.Min.Y + y*sb.Dy()/size
		for x := 0; x < size; x++ {
			sx := sb.Min.X + x*sb.Dx()/size
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DefaultFace draws a generic pixel face whose colors derive from seed.
func DefaultFace(seed string) image.Image {
	h := sha256.Sum256([]byte(seed))
	skins := []color.NRGBA{{0xf3, 0xc7, 0x9a, 255}, {0xe0, 0xac, 0x7e, 255}, {0xc6, 0x8d, 0x62, 255}, {0x9c, 0x6a, 0x47, 255}, {0x6f, 0x4a, 0x31, 255}}
	hairs := []color.NRGBA{{0x3b, 0x2a, 0x1e, 255}, {0x1f, 0x1a, 0x17, 255}, {0x8a, 0x5a, 0x2b, 255}, {0xd8, 0xb0, 0x5c, 255}, {0xa3, 0x3b, 0x20, 255}, {0x5c, 0x6b, 0x7a, 255}}
	eyes := []color.NRGBA{{0x3a, 0x5f, 0xc8, 255}, {0x4b, 0x8b, 0x3b, 255}, {0x5d, 0x3a, 0x1a, 255}, {0x6a, 0x6a, 0x8c, 255}}
	skin := skins[int(h[0])%len(skins)]
	hair := hairs[int(h[1])%len(hairs)]
	eye := eyes[int(h[2])%len(eyes)]
	shade := func(c color.NRGBA, f float64) color.NRGBA {
		return color.NRGBA{uint8(float64(c.R) * f), uint8(float64(c.G) * f), uint8(float64(c.B) * f), 255}
	}
	white := color.NRGBA{0xf5, 0xf5, 0xf5, 255}
	mouth := shade(skin, 0.55)
	layout := []string{
		"HHHHHHHH",
		"HHHHHHHH",
		"HSSSSSSH",
		"SSSSSSSS",
		"SWESSEWS",
		"SSSNNSSS",
		"SSMMMMSS",
		"SSSSSSSS",
	}
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y, row := range layout {
		for x, c := range row {
			var col color.NRGBA
			switch c {
			case 'H':
				col = shade(hair, 0.9+0.2*float64((x+y)%2))
			case 'S':
				col = skin
			case 'W':
				col = white
			case 'E':
				col = eye
			case 'N':
				col = shade(skin, 0.85)
			case 'M':
				col = mouth
			}
			img.SetNRGBA(x, y, col)
		}
	}
	return img
}
