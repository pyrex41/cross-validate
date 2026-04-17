package shen

import (
	"fmt"
	"strconv"
	"unicode"
)

// ---------------------------------------------------------------------------
// Tokenizer
// ---------------------------------------------------------------------------

type tokKind int

const (
	tokLParen tokKind = iota
	tokRParen
	tokLBracket
	tokRBracket
	tokPipe
	tokString
	tokNumber
	tokSymbol
	tokEOF
)

type token struct {
	kind tokKind
	sval string
	nval float64
}

type tokenizer struct {
	input []rune
	pos   int
}

func newTokenizer(input string) *tokenizer {
	return &tokenizer{input: []rune(input)}
}

func (t *tokenizer) atEnd() bool { return t.pos >= len(t.input) }

func (t *tokenizer) peek() rune {
	if t.atEnd() {
		return 0
	}
	return t.input[t.pos]
}

func (t *tokenizer) skipWhitespace() {
	for !t.atEnd() {
		ch := t.input[t.pos]
		switch {
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			t.pos++
		case ch == '\\' && t.pos+1 < len(t.input) && t.input[t.pos+1] == '*':
			t.skipComment()
		case ch == '{':
			t.skipTypeSig()
		default:
			return
		}
	}
}

func (t *tokenizer) skipComment() {
	t.pos += 2 // skip \*
	depth := 1
	for !t.atEnd() && depth > 0 {
		if t.pos+1 < len(t.input) && t.input[t.pos] == '*' && t.input[t.pos+1] == '\\' {
			depth--
			t.pos += 2
		} else if t.pos+1 < len(t.input) && t.input[t.pos] == '\\' && t.input[t.pos+1] == '*' {
			depth++
			t.pos += 2
		} else {
			t.pos++
		}
	}
}

func (t *tokenizer) skipTypeSig() {
	t.pos++ // skip {
	depth := 1
	for !t.atEnd() && depth > 0 {
		if t.input[t.pos] == '{' {
			depth++
		} else if t.input[t.pos] == '}' {
			depth--
		}
		t.pos++
	}
}

func isDelim(ch rune) bool {
	return ch == '(' || ch == ')' || ch == '[' || ch == ']' ||
		ch == '{' || ch == '}' || ch == '"' || ch == '|' ||
		ch == ';' || unicode.IsSpace(ch)
}

func (t *tokenizer) next() token {
	t.skipWhitespace()
	if t.atEnd() {
		return token{kind: tokEOF}
	}
	ch := t.input[t.pos]
	switch ch {
	case '(':
		t.pos++
		return token{kind: tokLParen}
	case ')':
		t.pos++
		return token{kind: tokRParen}
	case '[':
		t.pos++
		return token{kind: tokLBracket}
	case ']':
		t.pos++
		return token{kind: tokRBracket}
	case '|':
		t.pos++
		return token{kind: tokPipe}
	case ';':
		t.pos++ // treat as whitespace-like separator inside datatype
		return t.next()
	case '"':
		return t.readString()
	default:
		return t.readAtom()
	}
}

func (t *tokenizer) readString() token {
	t.pos++ // skip opening "
	var result []rune
	for !t.atEnd() {
		ch := t.input[t.pos]
		if ch == '\\' && t.pos+1 < len(t.input) {
			t.pos++
			next := t.input[t.pos]
			switch next {
			case 'n':
				result = append(result, '\n')
			case 't':
				result = append(result, '\t')
			case '"':
				result = append(result, '"')
			case '\\':
				result = append(result, '\\')
			default:
				result = append(result, '\\', next)
			}
			t.pos++
		} else if ch == '"' {
			t.pos++
			return token{kind: tokString, sval: string(result)}
		} else {
			result = append(result, ch)
			t.pos++
		}
	}
	return token{kind: tokString, sval: string(result)} // unterminated
}

func (t *tokenizer) readAtom() token {
	start := t.pos
	for !t.atEnd() && !isDelim(t.input[t.pos]) {
		t.pos++
	}
	text := string(t.input[start:t.pos])
	// Try number
	if n, err := strconv.ParseFloat(text, 64); err == nil {
		return token{kind: tokNumber, nval: n, sval: text}
	}
	return token{kind: tokSymbol, sval: text}
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

type parser struct {
	tokens []token
	pos    int
}

// Parse parses all top-level values from input text.
func Parse(input string) ([]Value, error) {
	tok := newTokenizer(input)
	var tokens []token
	for {
		t := tok.next()
		tokens = append(tokens, t)
		if t.kind == tokEOF {
			break
		}
	}
	p := &parser{tokens: tokens}
	var results []Value
	for p.peek().kind != tokEOF {
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		results = append(results, val)
	}
	return results, nil
}

func (p *parser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{kind: tokEOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) advance() token {
	t := p.tokens[p.pos]
	p.pos++
	return t
}

func (p *parser) parseValue() (Value, error) {
	tok := p.peek()
	switch tok.kind {
	case tokLParen:
		return p.parseForm()
	case tokLBracket:
		return p.parseList()
	case tokString:
		p.advance()
		return Str(tok.sval), nil
	case tokNumber:
		p.advance()
		return Num(tok.nval), nil
	case tokSymbol:
		p.advance()
		switch tok.sval {
		case "true":
			return Bool(true), nil
		case "false":
			return Bool(false), nil
		default:
			return Sym(tok.sval), nil
		}
	case tokEOF:
		return nil, fmt.Errorf("unexpected end of input")
	default:
		p.advance()
		return nil, fmt.Errorf("unexpected token kind %d (%q)", tok.kind, tok.sval)
	}
}

func (p *parser) parseForm() (Value, error) {
	startPos := p.pos
	p.advance() // skip (
	var elems []Value
	for p.peek().kind != tokRParen {
		if p.peek().kind == tokEOF {
			head := ""
			if len(elems) > 0 {
				head = PrintValue(elems[0])
			}
			return nil, fmt.Errorf("unterminated form (started at token %d, head=%s, %d elems so far)", startPos, head, len(elems))
		}
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		elems = append(elems, val)
	}
	p.advance() // skip )
	return Form(elems), nil
}

func (p *parser) parseList() (Value, error) {
	p.advance() // skip [
	var elems []Value
	var tail Value
	hasPipe := false

	for p.peek().kind != tokRBracket {
		if p.peek().kind == tokEOF {
			return nil, fmt.Errorf("unterminated list")
		}
		if p.peek().kind == tokPipe {
			p.advance() // skip |
			hasPipe = true
			var err error
			tail, err = p.parseValue()
			if err != nil {
				return nil, err
			}
			break
		}
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		elems = append(elems, val)
	}
	if p.peek().kind != tokRBracket {
		return nil, fmt.Errorf("expected ], got %q", p.peek().sval)
	}
	p.advance() // skip ]

	// Build cons chain
	var result Value
	if hasPipe {
		result = tail
	}
	for i := len(elems) - 1; i >= 0; i-- {
		result = &Cons{Car: elems[i], Cdr: result}
	}
	return result, nil
}
