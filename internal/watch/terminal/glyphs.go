package terminal

import (
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type Symbols struct{ Bullet, Pipe, Rule, Cross, Ellipsis string }

// Glyphs describes UI punctuation, independently of terminal color support.
// Modern terminals default to Unicode; dumb/non-Unicode locales fall back.
// SPANIEL_ASCII explicitly selects compatibility mode (1) or Unicode (0).
func Glyphs() Symbols {
	unicode := true
	switch strings.ToLower(os.Getenv("SPANIEL_ASCII")) {
	case "1", "true", "yes":
		unicode = false
	case "0", "false", "no":
	default:
		unicode = !strings.EqualFold(os.Getenv("TERM"), "dumb")
		for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
			locale := strings.ToLower(os.Getenv(key))
			if locale == "" {
				continue
			}
			if locale == "c" || locale == "posix" || strings.Contains(locale, ".") && !strings.Contains(locale, "utf8") && !strings.Contains(locale, "utf-8") {
				unicode = false
			}
			break
		}
	}
	if unicode {
		return Symbols{"•", "│", "─", "┼", "…"}
	}
	return Symbols{"*", "|", "-", "+", "..."}
}

func ColumnSeparator() string { return " " + Glyphs().Pipe + " " }

func Truncate(value string, width int) string {
	width = max(1, width)
	tail := Glyphs().Ellipsis
	if len(tail) == 3 && tail == "..." && width < 3 {
		tail = tail[:width]
	}
	return ansi.Truncate(value, width, tail)
}
