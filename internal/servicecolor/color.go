// Package servicecolor matches the browser's canonical service-color algorithm.
package servicecolor

import (
	"fmt"
	"math"
	"unicode/utf16"
)

type HSL struct{ Hue, Saturation, Lightness float64 }
type RGB struct{ R, G, B uint8 }

// Color hashes the full canonical name, including case and whitespace. JavaScript
// charCodeAt iterates UTF-16 code units, so astral characters require surrogate pairs.
func Color(name string) HSL {
	h := uint32(2166136261 ^ 3365)
	for _, unit := range utf16.Encode([]rune(name)) {
		h = (h ^ uint32(unit)) * 16777619
	}
	h = (h ^ (h >> 16)) * 0x85ebca6b
	h = (h ^ (h >> 13)) * 0xc2b2ae35
	h ^= h >> 16
	return HSL{float64(h%360) + float64((h>>16)%100)/100, float64(60 + (h>>9)%26), float64(48 + (h>>18)%13)}
}

func (c HSL) RGB() RGB {
	s, l := c.Saturation/100, c.Lightness/100
	chroma := (1 - math.Abs(2*l-1)) * s
	x := chroma * (1 - math.Abs(math.Mod(c.Hue/60, 2)-1))
	m := l - chroma/2
	var r, g, b float64
	switch {
	case c.Hue < 60:
		r, g = chroma, x
	case c.Hue < 120:
		r, g = x, chroma
	case c.Hue < 180:
		g, b = chroma, x
	case c.Hue < 240:
		g, b = x, chroma
	case c.Hue < 300:
		r, b = x, chroma
	default:
		r, b = chroma, x
	}
	return RGB{uint8(math.Round((r + m) * 255)), uint8(math.Round((g + m) * 255)), uint8(math.Round((b + m) * 255))}
}

// Text uses the same contrast lightness as ServiceNameBadge on the website.
func (c HSL) Text(dark bool) RGB {
	c.Lightness = 30
	if dark {
		c.Lightness = 78
	}
	return c.RGB()
}

func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }
