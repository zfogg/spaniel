package querydsl

import (
	"strings"
	"testing"
)

func TestParseBooleanPrecedence(t *testing.T) {
	n, err := Parse(`service:api && (attributes.http.status_code=500 || !"timeout")`)
	if err != nil || n.Op != "and" || n.Right.Op != "or" || n.Right.Right.Op != "not" || n.Right.Left.Field != "attributes.http.status_code" {
		t.Fatalf("node=%#v err=%v", n, err)
	}
}
func TestParseInvalid(t *testing.T) {
	for _, q := range []string{`a &&`, `(a OR b`, `service=`, `"bad`, `a | b`} {
		if _, err := Parse(q); err == nil {
			t.Errorf("accepted %q", q)
		}
	}
}

func TestParsePrecedenceAndLiterals(t *testing.T) {
	tests := []struct {
		name, input string
		check       func(*Node) bool
	}{
		{"AND before OR", `a OR b AND c`, func(n *Node) bool {
			return n.Op == "or" && n.Left.Value == "a" && n.Right.Op == "and" && n.Right.Left.Value == "b" && n.Right.Right.Value == "c"
		}},
		{"parentheses override", `(a OR b) AND c`, func(n *Node) bool { return n.Op == "and" && n.Left.Op == "or" && n.Right.Value == "c" }},
		{"NOT binds to comparison", `NOT attributes.env=prod OR service=api`, func(n *Node) bool {
			return n.Op == "or" && n.Left.Op == "not" && n.Left.Left.Field == "attributes.env" && n.Left.Left.Value == "prod"
		}},
		{"symbolic operators", `!a&&b||c`, func(n *Node) bool { return n.Op == "or" && n.Left.Op == "and" && n.Left.Left.Op == "not" }},
		{"quoted reserved word", `body="AND"`, func(n *Node) bool { return n.Op == "=" && n.Field == "body" && n.Value == "AND" }},
		{"escaped quoted value", `body:"say \"hello\""`, func(n *Node) bool { return n.Op == ":" && n.Value == `say "hello"` }},
		{"empty quoted value", `attributes.key=""`, func(n *Node) bool { return n.Field == "attributes.key" && n.Value == "" }},
		{"comparison operators", `duration_ns>=42`, func(n *Node) bool { return n.Op == ">=" && n.Field == "duration_ns" && n.Value == "42" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := Parse(tt.input)
			if err != nil || n == nil || !tt.check(n) {
				t.Fatalf("Parse(%q) = %#v, %v", tt.input, n, err)
			}
		})
	}
}

func TestParseLimitsAndMalformedOperators(t *testing.T) {
	for _, input := range []string{`a OR`, `a && && b`, `()`, `NOT`, `a b`, `service!=`, `a & b`, `a || (b`, strings.Repeat("x", 8193), strings.Repeat("!", 65) + "x", strings.Repeat("a AND ", 257) + "a"} {
		t.Run(input[:min(len(input), 32)], func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatalf("accepted invalid expression %q", input)
			}
		})
	}
	n, err := Parse(" \t\n ")
	if err != nil || n != nil {
		t.Fatalf("empty expression = %#v, %v", n, err)
	}
}

func TestParseEachComparisonOperator(t *testing.T) {
	for _, op := range []string{":", "=", "!=", "<", ">", "<=", ">="} {
		t.Run(op, func(t *testing.T) {
			n, err := Parse("attributes.http.status_code" + op + "500")
			if err != nil || n == nil || n.Op != op || n.Field != "attributes.http.status_code" || n.Value != "500" {
				t.Fatalf("operator %q: %#v, %v", op, n, err)
			}
		})
	}
}
