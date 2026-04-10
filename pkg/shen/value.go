// Package shen provides an in-process Shen language runtime for Go.
// It implements the subset of Shen needed by the xpc type-checking kernel:
// pattern-matching function definitions, list operations, string operations,
// and lexical closures.
package shen

import (
	"fmt"
	"strings"
)

// Value is the universal type for all Shen data.
// nil represents the empty list [].
type Value interface{}

// Sym is a Shen symbol.
type Sym string

// Str is a Shen string.
type Str string

// Num is a Shen number.
type Num float64

// Bool is a Shen boolean.
type Bool bool

// Cons is a cons cell (list node).
type Cons struct{ Car, Cdr Value }

// Form is a parsed (...) application. Exists only in the AST; never a runtime result.
type Form []Value

// Lambda is a one-parameter closure.
type Lambda struct {
	Param string
	Body  Value
	Env   *Env
}

// BuiltinFn is a Go function callable from Shen.
type BuiltinFn struct {
	Name  string
	Arity int // -1 = variadic
	Fn    func([]Value) (Value, error)
}

// Defun is a user-defined function with pattern-matching cases.
type Defun struct {
	Name  string
	Arity int
	Cases []DefunCase
}

// DefunCase is one clause of a pattern-matching function.
type DefunCase struct {
	Patterns []Value
	Guard    Value // nil means no guard
	Body     Value
}

// ---------------------------------------------------------------------------
// Environment (lexical scope)
// ---------------------------------------------------------------------------

// Env is a lexical scope chain.
type Env struct {
	bindings map[string]Value
	parent   *Env
}

// NewEnv creates a child scope.
func NewEnv(parent *Env) *Env {
	return &Env{bindings: make(map[string]Value), parent: parent}
}

// Get looks up a binding, walking the scope chain.
func (e *Env) Get(name string) (Value, bool) {
	if v, ok := e.bindings[name]; ok {
		return v, true
	}
	if e.parent != nil {
		return e.parent.Get(name)
	}
	return nil, false
}

// Set binds a name in this scope.
func (e *Env) Set(name string, val Value) { e.bindings[name] = val }

// ---------------------------------------------------------------------------
// Value helpers
// ---------------------------------------------------------------------------

// IsList returns true if v is a proper list (chain of Cons ending in nil).
func IsList(v Value) bool {
	for v != nil {
		c, ok := v.(*Cons)
		if !ok {
			return false
		}
		v = c.Cdr
	}
	return true
}

// ToSlice converts a proper list to a Go slice.
func ToSlice(v Value) []Value {
	var out []Value
	for v != nil {
		c, ok := v.(*Cons)
		if !ok {
			break
		}
		out = append(out, c.Car)
		v = c.Cdr
	}
	return out
}

// FromSlice builds a proper list from a Go slice.
func FromSlice(vals []Value) Value {
	var v Value // nil = []
	for i := len(vals) - 1; i >= 0; i-- {
		v = &Cons{Car: vals[i], Cdr: v}
	}
	return v
}

// ListLen returns the length of a proper list.
func ListLen(v Value) int {
	n := 0
	for v != nil {
		c, ok := v.(*Cons)
		if !ok {
			break
		}
		n++
		v = c.Cdr
	}
	return n
}

// Truthy follows Shen semantics: false and nil are falsy, everything else truthy.
func Truthy(v Value) bool {
	switch x := v.(type) {
	case Bool:
		return bool(x)
	case nil:
		return false
	default:
		return true
	}
}

// ValueEqual is structural equality for Shen values.
func ValueEqual(a, b Value) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case Sym:
		y, ok := b.(Sym)
		return ok && x == y
	case Str:
		y, ok := b.(Str)
		return ok && x == y
	case Num:
		y, ok := b.(Num)
		return ok && x == y
	case Bool:
		y, ok := b.(Bool)
		return ok && x == y
	case *Cons:
		y, ok := b.(*Cons)
		return ok && ValueEqual(x.Car, y.Car) && ValueEqual(x.Cdr, y.Cdr)
	}
	return a == b
}

// PrintValue returns a human-readable representation.
func PrintValue(v Value) string {
	switch x := v.(type) {
	case nil:
		return "[]"
	case Sym:
		return string(x)
	case Str:
		return fmt.Sprintf("%q", string(x))
	case Num:
		if x == Num(int(x)) {
			return fmt.Sprintf("%d", int(x))
		}
		return fmt.Sprintf("%g", float64(x))
	case Bool:
		if x {
			return "true"
		}
		return "false"
	case *Cons:
		if IsList(v) {
			elems := ToSlice(v)
			parts := make([]string, len(elems))
			for i, e := range elems {
				parts[i] = PrintValue(e)
			}
			return "[" + strings.Join(parts, " ") + "]"
		}
		return fmt.Sprintf("[%s | %s]", PrintValue(x.Car), PrintValue(x.Cdr))
	case Form:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = PrintValue(e)
		}
		return "(" + strings.Join(parts, " ") + ")"
	case *Lambda:
		return "<lambda>"
	case *BuiltinFn:
		return "<builtin:" + x.Name + ">"
	case *Defun:
		return "<defun:" + x.Name + ">"
	default:
		return fmt.Sprintf("%v", v)
	}
}
