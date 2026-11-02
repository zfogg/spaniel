package terminal

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestGlyphCapabilitiesAreIndependentOfColor(t *testing.T) {
	t.Setenv("SPANIEL_ASCII", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_CTYPE", "")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "1")
	if Glyphs().Bullet != "•" || Glyphs().Pipe != "│" {
		t.Fatal("NO_COLOR disabled Unicode", Glyphs())
	}
	t.Setenv("TERM", "dumb")
	if Glyphs().Pipe != "|" {
		t.Fatal("dumb terminal did not fall back")
	}
	t.Setenv("TERM", "xterm")
	t.Setenv("LC_ALL", "C")
	if Glyphs().Bullet != "*" {
		t.Fatal("ASCII locale did not fall back")
	}
	t.Setenv("SPANIEL_ASCII", "0")
	if Glyphs().Pipe != "│" {
		t.Fatal("explicit Unicode override ignored")
	}
	t.Setenv("SPANIEL_ASCII", "1")
	if Glyphs().Pipe != "|" {
		t.Fatal("explicit ASCII override ignored")
	}
}

func TestTimestampGlyphModesKeepAgeAndDisplayWidth(t *testing.T) {
	now := time.Date(2026, 11, 2, 21, 5, 0, 0, time.UTC)
	for _, mode := range []string{"0", "1"} {
		t.Setenv("SPANIEL_ASCII", mode)
		value := Timestamp(now.Add(-2*time.Minute).UnixNano(), "2006-01-02 15:04:05.000 MST", time.UTC, now)
		if !strings.Contains(value, " "+Glyphs().Bullet+" 2m ago") {
			t.Fatal(mode, value)
		}
		for _, separator := range []string{" • ", " * "} {
			input := "2026-11-02 21:03:00.000 UTC" + separator + "2m ago"
			for _, width := range []int{1, 2, 8, 18, 28, 60} {
				fit := FitTimestamp(input, width)
				if ansi.StringWidth(fit) > width || width >= 8 && !strings.Contains(fit, "2m ago") {
					t.Fatal(mode, width, fit)
				}
			}
		}
	}
}
