// Package querydsl parses Spaniel's boolean signal search expressions.
package querydsl

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

type Node struct {
	Op, Field, Value string
	Left, Right      *Node
}
type parser struct {
	tokens   []string
	i, depth int
}

// Parse accepts boolean combinations of words and field:value or field=value terms.
func Parse(input string) (*Node, error) {
	if len(input) > 8192 {
		return nil, fmt.Errorf("query is too long")
	}
	tokens, err := lex(input)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, nil
	}
	p := parser{tokens: tokens}
	n, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.i != len(tokens) {
		return nil, fmt.Errorf("unexpected %q", tokens[p.i])
	}
	return n, nil
}
func lex(s string) ([]string, error) {
	var t []string
	for i := 0; i < len(s); {
		if unicode.IsSpace(rune(s[i])) {
			i++
			continue
		}
		c := s[i]
		if c == '"' {
			start := i
			i++
			escaped := false
			for i < len(s) {
				if !escaped && s[i] == '"' {
					i++
					break
				}
				if !escaped && s[i] == '\\' {
					escaped = true
				} else {
					escaped = false
				}
				i++
			}
			if i > len(s) || s[i-1] != '"' {
				return nil, fmt.Errorf("unterminated string")
			}
			var v string
			if err := json.Unmarshal([]byte(s[start:i]), &v); err != nil {
				return nil, fmt.Errorf("invalid string: %w", err)
			}
			t = append(t, "\x00"+v)
			continue
		}
		if i+1 < len(s) && (s[i:i+2] == "&&" || s[i:i+2] == "||" || s[i:i+2] == "!=" || s[i:i+2] == ">=" || s[i:i+2] == "<=") {
			t = append(t, s[i:i+2])
			i += 2
			continue
		}
		if strings.ContainsRune("()!:=<>", rune(c)) {
			t = append(t, string(c))
			i++
			continue
		}
		start := i
		for i < len(s) && !unicode.IsSpace(rune(s[i])) && !strings.ContainsRune("()!:=<>&|\"", rune(s[i])) {
			i++
		}
		if start == i {
			return nil, fmt.Errorf("unexpected character %q", s[i])
		}
		t = append(t, s[start:i])
		if len(t) > 512 {
			return nil, fmt.Errorf("query has too many tokens")
		}
	}
	return t, nil
}
func (p *parser) peek(v ...string) bool {
	if p.i >= len(p.tokens) {
		return false
	}
	for _, s := range v {
		if strings.EqualFold(p.tokens[p.i], s) {
			return true
		}
	}
	return false
}
func (p *parser) or() (*Node, error) {
	n, e := p.and()
	if e != nil {
		return nil, e
	}
	for p.peek("OR", "||") {
		p.i++
		r, e := p.and()
		if e != nil {
			return nil, e
		}
		n = &Node{Op: "or", Left: n, Right: r}
	}
	return n, nil
}
func (p *parser) and() (*Node, error) {
	n, e := p.unary()
	if e != nil {
		return nil, e
	}
	for p.peek("AND", "&&") {
		p.i++
		r, e := p.unary()
		if e != nil {
			return nil, e
		}
		n = &Node{Op: "and", Left: n, Right: r}
	}
	return n, nil
}
func (p *parser) unary() (*Node, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 64 {
		return nil, fmt.Errorf("query nesting too deep")
	}
	if p.peek("NOT", "!") {
		p.i++
		n, e := p.unary()
		return &Node{Op: "not", Left: n}, e
	}
	if p.peek("(") {
		p.i++
		n, e := p.or()
		if e != nil {
			return nil, e
		}
		if !p.peek(")") {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		p.i++
		return n, nil
	}
	if p.i >= len(p.tokens) || p.peek(")", "AND", "OR", "&&", "||", ":", "=", "!=", ">", "<", ">=", "<=") {
		return nil, fmt.Errorf("expected search term")
	}
	term := p.tokens[p.i]
	p.i++
	if p.peek(":", "=", "!=", ">", "<", ">=", "<=") {
		op := p.tokens[p.i]
		p.i++
		if p.i >= len(p.tokens) || p.peek("(", ")", "AND", "OR", "&&", "||") {
			return nil, fmt.Errorf("missing value for %s", term)
		}
		value := strings.TrimPrefix(p.tokens[p.i], "\x00")
		p.i++
		return &Node{Op: op, Field: term, Value: value}, nil
	}
	return &Node{Op: "text", Value: strings.TrimPrefix(term, "\x00")}, nil
}
