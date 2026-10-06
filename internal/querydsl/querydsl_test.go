package querydsl

import (
	"strings"
	"testing"
)

func TestParseCompileParameterized(t *testing.T) {
	q, err := Parse(`p95(spans.duration) by service_name where service = $service_name and attributes.http.route = "/orders"`)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compile(q, map[string]string{"service_name": "checkout"}, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c.SQL, "checkout") || strings.Contains(c.SQL, "/orders") {
		t.Fatalf("values leaked into SQL: %s", c.SQL)
	}
	if len(c.Args) != 3 {
		t.Fatalf("args=%v", c.Args)
	}
}

func TestCompileRejectsUnsetVariable(t *testing.T) {
	q, err := Parse("count(spans) where service_name = $service_name")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(q, map[string]string{}, ""); err == nil {
		t.Fatal("expected an unset variable to be rejected")
	}
}

func TestParseRejectsSQL(t *testing.T) {
	if _, err := Parse(`count(spans) where service = "x"; DROP TABLE spans`); err == nil {
		t.Fatal("accepted SQL injection")
	}
}

func TestParseRejectsUnknownVariable(t *testing.T) {
	if _, err := Parse(`count(spans) where service = $not_a_variable`); err == nil {
		t.Fatal("accepted unknown variable")
	}
}
