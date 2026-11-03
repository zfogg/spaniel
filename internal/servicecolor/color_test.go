package servicecolor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBrowserParity(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../testdata/service-colors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name                        string
			Hue, Saturation, Lightness  float64
			Swatch, LightText, DarkText [3]uint8
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) < 16 {
		t.Fatal("missing browser vectors")
	}
	for _, v := range fixture.Cases {
		t.Run(v.Name, func(t *testing.T) {
			want := HSL{v.Hue, v.Saturation, v.Lightness}
			if got := Color(v.Name); got != want {
				t.Fatalf("UTF-16 hash: got %+v, browser %+v", got, want)
			}
			for _, mode := range []struct {
				got  RGB
				want [3]uint8
			}{{want.RGB(), v.Swatch}, {want.Text(false), v.LightText}, {want.Text(true), v.DarkText}} {
				if [3]uint8{mode.got.R, mode.got.G, mode.got.B} != mode.want {
					t.Fatalf("RGB: got %+v, browser %v", mode.got, mode.want)
				}
			}
			Color("new-service")
			if Color(v.Name) != want {
				t.Fatal("arrival order changed the color")
			}
		})
	}
}
