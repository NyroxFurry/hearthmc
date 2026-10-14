package mapr

import (
	"image"
	"image/color"
	"math"
)

// RenderOptions controls how a region is drawn.
type RenderOptions struct {
	Ceiling int // >0: cave mode below this Y (Nether)
}

func clamp8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func scale(c RGB, f float64) RGB {
	return RGB{clamp8(float64(c.R) * f), clamp8(float64(c.G) * f), clamp8(float64(c.B) * f)}
}

// surfaceColor computes the final color of a column before relief shading.
func surfaceColor(bi *BlockInfo, tint biomeTint, depth int) RGB {
	var c RGB
	switch bi.Tint {
	case TintGrass:
		c = scale(hexRGB(tint.grass), 0.93)
	case TintFoliage:
		c = scale(hexRGB(tint.foliage), 0.82)
	case TintWater:
		c = hexRGB(tint.water)
	default:
		c = bi.Color
	}
	if depth > 0 {
		water := mix(hexRGB(tint.water), RGB{0x2a, 0x4f, 0xa8}, 0.25)
		if bi.Water { // ocean deeper than the scan limit
			return scale(water, 0.78)
		}
		a := math.Min(0.92, 0.5+float64(depth)*0.055)
		c = mix(c, water, a)
	}
	return c
}

// RenderRegion draws one region (32×32 chunks) as a 512×512 image.
// Returns the image and the number of rendered chunks.
func RenderRegion(path string, opt RenderOptions) (*image.NRGBA, int, error) {
	reg, err := OpenRegion(path)
	if err != nil {
		return nil, 0, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	heights := make([]int32, 512*512)
	for i := range heights {
		heights[i] = math.MinInt32
	}
	colors := make([]RGB, 512*512)
	chunks := 0
	for cz := 0; cz < 32; cz++ {
		for cx := 0; cx < 32; cx++ {
			root, err := reg.Chunk(cx, cz)
			if err != nil {
				continue
			}
			ch := parseChunk(root)
			if ch == nil {
				continue
			}
			chunks++
			for z := 0; z < 16; z++ {
				for x := 0; x < 16; x++ {
					bi, y, depth, waterTop, ok := ch.column(x, z, opt.Ceiling)
					if !ok {
						continue
					}
					px, pz := cx*16+x, cz*16+z
					tint := ch.biome(x, y, z)
					h := y
					if depth > 0 {
						h = waterTop
					}
					idx := pz*512 + px
					heights[idx] = int32(h)
					colors[idx] = surfaceColor(bi, tint, depth)
				}
			}
		}
	}
	if chunks == 0 {
		return img, 0, nil
	}
	// relief shading against the north-west neighbour, like Minecraft maps
	for z := 0; z < 512; z++ {
		for x := 0; x < 512; x++ {
			idx := z*512 + x
			h := heights[idx]
			if h == math.MinInt32 {
				continue
			}
			ref := h
			if z > 0 && heights[idx-512] != math.MinInt32 {
				ref = heights[idx-512]
			}
			ref2 := h
			if x > 0 && heights[idx-1] != math.MinInt32 {
				ref2 = heights[idx-1]
			}
			d := float64(h-ref)*0.7 + float64(h-ref2)*0.3
			f := 1 + math.Max(-4, math.Min(4, d))*0.065
			c := scale(colors[idx], f)
			img.SetNRGBA(x, z, color.NRGBA{c.R, c.G, c.B, 255})
		}
	}
	return img, chunks, nil
}

// downscale halves an image (averaging 2×2 blocks, alpha aware) into dst at (ox, oy).
func downscale(dst *image.NRGBA, src *image.NRGBA, ox, oy int) {
	sb := src.Bounds()
	for y := 0; y < sb.Dy()/2; y++ {
		for x := 0; x < sb.Dx()/2; x++ {
			var r, g, b, a, n uint32
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					c := src.NRGBAAt(sb.Min.X+x*2+dx, sb.Min.Y+y*2+dy)
					if c.A == 0 {
						continue
					}
					r += uint32(c.R) * uint32(c.A)
					g += uint32(c.G) * uint32(c.A)
					b += uint32(c.B) * uint32(c.A)
					a += uint32(c.A)
					n++
				}
			}
			if a == 0 {
				continue
			}
			dst.SetNRGBA(ox+x, oy+y, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / 4)})
			_ = n
		}
	}
}
