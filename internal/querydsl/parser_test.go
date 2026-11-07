package querydsl

import "testing"

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
