package terminal

import "testing"

func TestExplicitLinePoliciesRemoveTerminalCommandsAndBidiControls(t *testing.T) {
	const input = "\x1b]0;title\a\x1b[31mfirst\x1b[0m\nsecond\tvalue\u202e\u2028end"
	if got := SingleLine(input); got != "first second value end" {
		t.Fatal(got)
	}
	if got := Multiline(input); got != "first\nsecond value end" {
		t.Fatal(got)
	}
}
