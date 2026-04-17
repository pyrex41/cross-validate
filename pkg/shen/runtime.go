package shen

import (
	"fmt"
	"strings"
)

// Runtime wraps the Evaluator and registers all built-in primitives.
type Runtime struct {
	*Evaluator
}

// NewRuntime creates a Shen runtime with all builtins registered.
func NewRuntime() *Runtime {
	rt := &Runtime{Evaluator: NewEvaluator()}
	rt.registerBuiltins()
	return rt
}

func (rt *Runtime) defBuiltin(name string, arity int, fn func([]Value) (Value, error)) {
	rt.globals[name] = &BuiltinFn{Name: name, Arity: arity, Fn: fn}
}

func (rt *Runtime) registerBuiltins() {
	// --- cons / list ---
	rt.defBuiltin("cons", 2, func(a []Value) (Value, error) {
		return &Cons{Car: a[0], Cdr: a[1]}, nil
	})
	rt.defBuiltin("hd", 1, func(a []Value) (Value, error) {
		if c, ok := a[0].(*Cons); ok {
			return c.Car, nil
		}
		return nil, fmt.Errorf("hd: not a cons: %s", PrintValue(a[0]))
	})
	rt.defBuiltin("tl", 1, func(a []Value) (Value, error) {
		if c, ok := a[0].(*Cons); ok {
			return c.Cdr, nil
		}
		return nil, fmt.Errorf("tl: not a cons: %s", PrintValue(a[0]))
	})
	rt.defBuiltin("append", 2, func(a []Value) (Value, error) {
		if a[0] == nil {
			return a[1], nil
		}
		elems := ToSlice(a[0])
		result := a[1]
		for i := len(elems) - 1; i >= 0; i-- {
			result = &Cons{Car: elems[i], Cdr: result}
		}
		return result, nil
	})
	rt.defBuiltin("length", 1, func(a []Value) (Value, error) {
		return Num(float64(ListLen(a[0]))), nil
	})
	rt.defBuiltin("map", 2, func(a []Value) (Value, error) {
		fn, list := a[0], a[1]
		var results []Value
		for list != nil {
			c, ok := list.(*Cons)
			if !ok {
				break
			}
			r, err := rt.Apply(fn, []Value{c.Car})
			if err != nil {
				return nil, err
			}
			results = append(results, r)
			list = c.Cdr
		}
		return FromSlice(results), nil
	})
	rt.defBuiltin("reverse", 1, func(a []Value) (Value, error) {
		elems := ToSlice(a[0])
		for i, j := 0, len(elems)-1; i < j; i, j = i+1, j-1 {
			elems[i], elems[j] = elems[j], elems[i]
		}
		return FromSlice(elems), nil
	})

	// --- equality / comparison ---
	rt.defBuiltin("=", 2, func(a []Value) (Value, error) {
		return Bool(ValueEqual(a[0], a[1])), nil
	})
	rt.defBuiltin("<", 2, func(a []Value) (Value, error) {
		return Bool(asNum(a[0]) < asNum(a[1])), nil
	})
	rt.defBuiltin(">", 2, func(a []Value) (Value, error) {
		return Bool(asNum(a[0]) > asNum(a[1])), nil
	})
	rt.defBuiltin(">=", 2, func(a []Value) (Value, error) {
		return Bool(asNum(a[0]) >= asNum(a[1])), nil
	})
	rt.defBuiltin("<=", 2, func(a []Value) (Value, error) {
		return Bool(asNum(a[0]) <= asNum(a[1])), nil
	})

	// --- arithmetic ---
	rt.defBuiltin("+", 2, func(a []Value) (Value, error) {
		return Num(asNum(a[0]) + asNum(a[1])), nil
	})
	rt.defBuiltin("-", 2, func(a []Value) (Value, error) {
		return Num(asNum(a[0]) - asNum(a[1])), nil
	})
	rt.defBuiltin("*", 2, func(a []Value) (Value, error) {
		return Num(asNum(a[0]) * asNum(a[1])), nil
	})
	rt.defBuiltin("/", 2, func(a []Value) (Value, error) {
		b := asNum(a[1])
		if b == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return Num(asNum(a[0]) / b), nil
	})

	// --- string ---
	rt.defBuiltin("cn", 2, func(a []Value) (Value, error) {
		return Str(asStr(a[0]) + asStr(a[1])), nil
	})
	rt.defBuiltin("str", 1, func(a []Value) (Value, error) {
		return Str(valueToStr(a[0])), nil
	})
	rt.defBuiltin("intern", 1, func(a []Value) (Value, error) {
		return Sym(asStr(a[0])), nil
	})
	rt.defBuiltin("explode", 1, func(a []Value) (Value, error) {
		s := asStr(a[0])
		runes := []rune(s)
		vals := make([]Value, len(runes))
		for i, r := range runes {
			vals[i] = Str(string(r))
		}
		return FromSlice(vals), nil
	})
	rt.defBuiltin("implode", 1, func(a []Value) (Value, error) {
		elems := ToSlice(a[0])
		var sb strings.Builder
		for _, e := range elems {
			sb.WriteString(asStr(e))
		}
		return Str(sb.String()), nil
	})
	rt.defBuiltin("pos", 2, func(a []Value) (Value, error) {
		s := asStr(a[0])
		n := int(asNum(a[1]))
		runes := []rune(s)
		if n < 0 || n >= len(runes) {
			return nil, fmt.Errorf("pos: index %d out of range", n)
		}
		return Str(string(runes[n])), nil
	})
	rt.defBuiltin("tlstr", 1, func(a []Value) (Value, error) {
		s := asStr(a[0])
		if len(s) <= 1 {
			return Str(""), nil
		}
		runes := []rune(s)
		return Str(string(runes[1:])), nil
	})
	rt.defBuiltin("string->n", 1, func(a []Value) (Value, error) {
		s := asStr(a[0])
		if len(s) == 0 {
			return nil, fmt.Errorf("string->n: empty string")
		}
		return Num(float64([]rune(s)[0])), nil
	})
	rt.defBuiltin("n->string", 1, func(a []Value) (Value, error) {
		return Str(string(rune(int(asNum(a[0]))))), nil
	})
	rt.defBuiltin("@s", 2, func(a []Value) (Value, error) {
		return Str(asStr(a[0]) + asStr(a[1])), nil
	})

	// make-string: Shen formatted string (~S = write, ~A = display, ~% = newline)
	rt.defBuiltin("make-string", -1, func(a []Value) (Value, error) {
		if len(a) == 0 {
			return Str(""), nil
		}
		template := asStr(a[0])
		argIdx := 1
		var sb strings.Builder
		runes := []rune(template)
		for i := 0; i < len(runes); i++ {
			if runes[i] == '~' && i+1 < len(runes) {
				switch runes[i+1] {
				case 'S':
					if argIdx < len(a) {
						sb.WriteString(PrintValue(a[argIdx]))
						argIdx++
					}
					i++
				case 'A':
					if argIdx < len(a) {
						sb.WriteString(asStr(a[argIdx]))
						argIdx++
					}
					i++
				case '%':
					sb.WriteRune('\n')
					i++
				case 'R':
					sb.WriteRune('\r')
					i++
				default:
					sb.WriteRune('~')
				}
			} else {
				sb.WriteRune(runes[i])
			}
		}
		return Str(sb.String()), nil
	})

	// --- type predicates ---
	rt.defBuiltin("cons?", 1, func(a []Value) (Value, error) {
		_, ok := a[0].(*Cons)
		return Bool(ok), nil
	})
	rt.defBuiltin("number?", 1, func(a []Value) (Value, error) {
		_, ok := a[0].(Num)
		return Bool(ok), nil
	})
	rt.defBuiltin("string?", 1, func(a []Value) (Value, error) {
		_, ok := a[0].(Str)
		return Bool(ok), nil
	})
	rt.defBuiltin("symbol?", 1, func(a []Value) (Value, error) {
		_, ok := a[0].(Sym)
		return Bool(ok), nil
	})
	rt.defBuiltin("boolean?", 1, func(a []Value) (Value, error) {
		_, ok := a[0].(Bool)
		return Bool(ok), nil
	})
	rt.defBuiltin("empty?", 1, func(a []Value) (Value, error) {
		return Bool(a[0] == nil), nil
	})
	rt.defBuiltin("element?", 2, func(a []Value) (Value, error) {
		item, list := a[0], a[1]
		for list != nil {
			c, ok := list.(*Cons)
			if !ok {
				break
			}
			if ValueEqual(item, c.Car) {
				return Bool(true), nil
			}
			list = c.Cdr
		}
		return Bool(false), nil
	})

	// --- I/O (mostly no-ops for embedded runtime) ---
	rt.defBuiltin("pr", 2, func(a []Value) (Value, error) { return a[0], nil })
	rt.defBuiltin("print", 1, func(a []Value) (Value, error) { return a[0], nil })
	rt.defBuiltin("read", 1, func(a []Value) (Value, error) {
		return nil, fmt.Errorf("read: not supported in embedded runtime")
	})
	rt.defBuiltin("stinput", 0, func(a []Value) (Value, error) { return Sym("stdin"), nil })
	rt.defBuiltin("stoutput", 0, func(a []Value) (Value, error) { return Sym("stdout"), nil })
	rt.defBuiltin("sterror", 0, func(a []Value) (Value, error) { return Sym("stderr"), nil })
	rt.defBuiltin("cd", 1, func(a []Value) (Value, error) {
		dir := asStr(a[0])
		if len(rt.loadPath) > 0 {
			rt.loadPath[len(rt.loadPath)-1] = dir
		} else {
			rt.loadPath = append(rt.loadPath, dir)
		}
		return Str(dir), nil
	})

	// --- error ---
	rt.defBuiltin("error", 1, func(a []Value) (Value, error) {
		return nil, fmt.Errorf("%s", asStr(a[0]))
	})
	rt.defBuiltin("simple-error", 1, func(a []Value) (Value, error) {
		return nil, fmt.Errorf("%s", asStr(a[0]))
	})
	rt.defBuiltin("error-to-string", 1, func(a []Value) (Value, error) {
		return Str(asStr(a[0])), nil
	})

	// --- global state ---
	rt.defBuiltin("value", 1, func(a []Value) (Value, error) {
		var name string
		switch v := a[0].(type) {
		case Sym:
			name = string(v)
		case Str:
			name = string(v)
		default:
			return nil, fmt.Errorf("value: expected symbol or string, got %T", a[0])
		}
		if v, ok := rt.globals[name]; ok {
			return v, nil
		}
		return nil, fmt.Errorf("value: unbound %s", name)
	})
	rt.defBuiltin("set", 2, func(a []Value) (Value, error) {
		name := asStr(a[0])
		rt.globals[name] = a[1]
		return a[1], nil
	})

	// --- shen-specific helpers used by kernel ---
	rt.defBuiltin("string-downcase", 1, func(a []Value) (Value, error) {
		return Str(strings.ToLower(asStr(a[0]))), nil
	})
	rt.defBuiltin("string-upcase", 1, func(a []Value) (Value, error) {
		return Str(strings.ToUpper(asStr(a[0]))), nil
	})
	rt.defBuiltin("shen.split-string", 2, func(a []Value) (Value, error) {
		s := asStr(a[0])
		delim := asStr(a[1])
		parts := strings.Split(s, delim)
		vals := make([]Value, len(parts))
		for i, p := range parts {
			vals[i] = Str(p)
		}
		return FromSlice(vals), nil
	})

	// trap-error: catch errors
	rt.defBuiltin("trap-error", 2, func(a []Value) (Value, error) {
		// First arg should be a frozen expression, second a handler lambda.
		// For simplicity, evaluate args directly.
		// In practice, trap-error is a special form, but handling it as a
		// builtin works when the kernel doesn't rely on lazy evaluation of the first arg.
		return a[0], nil
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func asNum(v Value) float64 {
	if n, ok := v.(Num); ok {
		return float64(n)
	}
	return 0
}

func asStr(v Value) string {
	switch s := v.(type) {
	case Str:
		return string(s)
	case Sym:
		return string(s)
	case Num:
		if s == Num(int(s)) {
			return fmt.Sprintf("%d", int(s))
		}
		return fmt.Sprintf("%g", float64(s))
	case Bool:
		if s {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return PrintValue(v)
	}
}

func valueToStr(v Value) string {
	switch s := v.(type) {
	case Str:
		return string(s)
	case Sym:
		return string(s)
	case Num:
		if s == Num(int(s)) {
			return fmt.Sprintf("%d", int(s))
		}
		return fmt.Sprintf("%g", float64(s))
	case Bool:
		if s {
			return "true"
		}
		return "false"
	default:
		return PrintValue(v)
	}
}
