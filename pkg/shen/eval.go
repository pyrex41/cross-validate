package shen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Evaluator is the Shen evaluation engine.
type Evaluator struct {
	globals  map[string]Value
	loadPath []string // stack of directories for nested loads
}

// NewEvaluator creates an empty evaluator (no builtins).
func NewEvaluator() *Evaluator {
	return &Evaluator{globals: make(map[string]Value)}
}

// SetGlobal binds a name at the global level.
func (ev *Evaluator) SetGlobal(name string, val Value) { ev.globals[name] = val }

// GetGlobal looks up a global binding.
func (ev *Evaluator) GetGlobal(name string) (Value, bool) {
	v, ok := ev.globals[name]
	return v, ok
}

// currentDir returns the directory for resolving relative (load ...) paths.
func (ev *Evaluator) currentDir() string {
	if len(ev.loadPath) > 0 {
		return ev.loadPath[len(ev.loadPath)-1]
	}
	return "."
}

// LoadFile reads, parses, and evaluates a .shen file.
func (ev *Evaluator) LoadFile(path string) (Value, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	dir := filepath.Dir(path)
	ev.loadPath = append(ev.loadPath, dir)
	defer func() { ev.loadPath = ev.loadPath[:len(ev.loadPath)-1] }()

	exprs, err := Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var result Value
	for _, expr := range exprs {
		result, err = ev.Eval(expr, nil)
		if err != nil {
			return nil, fmt.Errorf("eval %s: %w", path, err)
		}
	}
	return result, nil
}

// Call invokes a named Shen function with the given arguments.
func (ev *Evaluator) Call(name string, args ...Value) (Value, error) {
	fn, ok := ev.globals[name]
	if !ok {
		return nil, fmt.Errorf("undefined function: %s", name)
	}
	return ev.Apply(fn, args)
}

// ---------------------------------------------------------------------------
// Eval
// ---------------------------------------------------------------------------

// Eval evaluates a Shen value/expression in the given environment.
func (ev *Evaluator) Eval(val Value, env *Env) (Value, error) {
	switch v := val.(type) {
	case nil:
		return nil, nil
	case Str:
		return v, nil
	case Num:
		return v, nil
	case Bool:
		return v, nil
	case Sym:
		return ev.evalSym(v, env)
	case *Cons:
		return ev.evalCons(v, env)
	case Form:
		return ev.evalForm(v, env)
	default:
		return v, nil // lambdas, builtins, defuns are self-evaluating
	}
}

func (ev *Evaluator) evalSym(s Sym, env *Env) (Value, error) {
	name := string(s)
	if env != nil {
		if v, ok := env.Get(name); ok {
			return v, nil
		}
	}
	if v, ok := ev.globals[name]; ok {
		return v, nil
	}
	// Self-evaluating: unbound symbols evaluate to themselves.
	// This is needed for pattern matching and data symbols like crd-fact, world, etc.
	return s, nil
}

// evalCons evaluates a cons cell in expression context: evaluate both halves.
func (ev *Evaluator) evalCons(c *Cons, env *Env) (Value, error) {
	car, err := ev.Eval(c.Car, env)
	if err != nil {
		return nil, err
	}
	cdr, err := ev.Eval(c.Cdr, env)
	if err != nil {
		return nil, err
	}
	return &Cons{Car: car, Cdr: cdr}, nil
}

// evalForm dispatches on special forms or falls through to function application.
func (ev *Evaluator) evalForm(f Form, env *Env) (Value, error) {
	if len(f) == 0 {
		return nil, nil
	}
	if sym, ok := f[0].(Sym); ok {
		switch string(sym) {
		case "define":
			return ev.evalDefine(f[1:])
		case "datatype":
			// No-op: datatype declarations are compile-time type rules.
			return Sym("datatype"), nil
		case "let":
			return ev.evalLet(f[1:], env)
		case "if":
			return ev.evalIf(f[1:], env)
		case "cond":
			return ev.evalCond(f[1:], env)
		case "and":
			return ev.evalAnd(f[1:], env)
		case "or":
			return ev.evalOr(f[1:], env)
		case "not":
			if len(f) < 2 {
				return nil, fmt.Errorf("not: need argument")
			}
			v, err := ev.Eval(f[1], env)
			if err != nil {
				return nil, err
			}
			return Bool(!Truthy(v)), nil
		case "do":
			return ev.evalDo(f[1:], env)
		case "/.":
			return ev.evalLambda(f[1:], env)
		case "lambda":
			return ev.evalLambda(f[1:], env)
		case "load":
			return ev.evalLoad(f[1:], env)
		case "freeze":
			if len(f) < 2 {
				return nil, fmt.Errorf("freeze: need body")
			}
			return &Lambda{Param: "", Body: f[1], Env: env}, nil
		case "value":
			if len(f) < 2 {
				return nil, fmt.Errorf("value: need argument")
			}
			sym, ok := f[1].(Sym)
			if !ok {
				// Evaluate if not a symbol
				v, err := ev.Eval(f[1], env)
				if err != nil {
					return nil, err
				}
				name := ""
				switch s := v.(type) {
				case Sym:
					name = string(s)
				case Str:
					name = string(s)
				default:
					return nil, fmt.Errorf("value: expected symbol, got %T", v)
				}
				if g, ok := ev.globals[name]; ok {
					return g, nil
				}
				return nil, fmt.Errorf("value: unbound %s", name)
			}
			if g, ok := ev.globals[string(sym)]; ok {
				return g, nil
			}
			return nil, fmt.Errorf("value: unbound %s", string(sym))
		case "thaw":
			if len(f) < 2 {
				return nil, fmt.Errorf("thaw: need argument")
			}
			thunk, err := ev.Eval(f[1], env)
			if err != nil {
				return nil, err
			}
			if lam, ok := thunk.(*Lambda); ok && lam.Param == "" {
				return ev.Eval(lam.Body, lam.Env)
			}
			return nil, fmt.Errorf("thaw: not a frozen value")
		}
	}
	return ev.evalApplication(f, env)
}

// ---------------------------------------------------------------------------
// Special forms
// ---------------------------------------------------------------------------

func (ev *Evaluator) evalDefine(args []Value) (Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("define: need function name")
	}
	nameSym, ok := args[0].(Sym)
	if !ok {
		return nil, fmt.Errorf("define: name must be a symbol, got %T", args[0])
	}
	name := string(nameSym)
	rest := args[1:]

	// Determine arity: count values before the first "->" symbol.
	arity := -1
	for i, v := range rest {
		if s, ok := v.(Sym); ok && string(s) == "->" {
			arity = i
			break
		}
	}
	if arity < 0 {
		return nil, fmt.Errorf("define %s: no -> found", name)
	}

	var cases []DefunCase
	i := 0
	for i < len(rest) {
		// Read arity pattern values
		if i+arity > len(rest) {
			break
		}
		patterns := make([]Value, arity)
		copy(patterns, rest[i:i+arity])
		i += arity

		// Expect ->
		if i >= len(rest) {
			break
		}
		if s, ok := rest[i].(Sym); !ok || string(s) != "->" {
			return nil, fmt.Errorf("define %s: expected -> at position %d, got %v", name, i, PrintValue(rest[i]))
		}
		i++

		// Read body (one expression)
		if i >= len(rest) {
			return nil, fmt.Errorf("define %s: expected body after ->", name)
		}
		body := rest[i]
		i++

		// Optional where guard
		var guard Value
		if i < len(rest) {
			if s, ok := rest[i].(Sym); ok && string(s) == "where" {
				i++
				if i >= len(rest) {
					return nil, fmt.Errorf("define %s: expected guard after where", name)
				}
				guard = rest[i]
				i++
			}
		}

		cases = append(cases, DefunCase{Patterns: patterns, Guard: guard, Body: body})
	}

	defun := &Defun{Name: name, Arity: arity, Cases: cases}
	ev.globals[name] = defun
	return Sym(name), nil
}

func (ev *Evaluator) evalLet(args []Value, env *Env) (Value, error) {
	// (let X Val Body) or (let X1 V1 X2 V2 ... Body) — Shen uses nested lets,
	// so the three-arg form suffices; the kernel uses chained lets which parse as:
	//   (let X1 V1 (let X2 V2 ... Body))
	// BUT Shen also supports a flat form: (let X1 V1 X2 V2 ... Body)
	// where every pair except the last is a binding.
	if len(args) < 3 {
		return nil, fmt.Errorf("let: need at least 3 args")
	}

	// Handle chained let: (let X1 V1 X2 V2 ... Body)
	newEnv := NewEnv(env)
	i := 0
	for i+2 < len(args) {
		varSym, ok := args[i].(Sym)
		if !ok {
			// Not a symbol — treat remaining as body
			break
		}
		varName := string(varSym)
		val, err := ev.Eval(args[i+1], newEnv)
		if err != nil {
			return nil, err
		}
		newEnv.Set(varName, val)
		i += 2
		// If the remaining is exactly 1 element, it's the body
		if i+2 >= len(args) {
			break
		}
		// Peek ahead: if next is not a symbol, body follows
		if _, ok := args[i].(Sym); !ok {
			break
		}
		// Check if the element after next looks like it could be a value followed by more;
		// the safest heuristic: if there are at least 3 more elements and args[i] is an uppercase sym,
		// continue binding. But actually Shen's let is always 3-arg (let X V Body).
		// The kernel uses: (let X1 V1 X2 V2 ... Body) as sugar.
		// We'll continue binding pairs as long as we have >= 3 remaining.
	}

	// Evaluate body (last element or remaining elements)
	if i >= len(args) {
		return nil, fmt.Errorf("let: missing body")
	}
	return ev.Eval(args[i], newEnv)
}

func (ev *Evaluator) evalIf(args []Value, env *Env) (Value, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("if: need cond, then, else")
	}
	cond, err := ev.Eval(args[0], env)
	if err != nil {
		return nil, err
	}
	if Truthy(cond) {
		return ev.Eval(args[1], env)
	}
	return ev.Eval(args[2], env)
}

func (ev *Evaluator) evalCond(args []Value, env *Env) (Value, error) {
	for _, clause := range args {
		elems := ToSlice(clause)
		if len(elems) != 2 {
			return nil, fmt.Errorf("cond: each clause must be a pair")
		}
		test, err := ev.Eval(elems[0], env)
		if err != nil {
			return nil, err
		}
		if Truthy(test) {
			return ev.Eval(elems[1], env)
		}
	}
	return nil, fmt.Errorf("cond: no clause matched")
}

func (ev *Evaluator) evalAnd(args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return Bool(true), nil
	}
	left, err := ev.Eval(args[0], env)
	if err != nil {
		return nil, err
	}
	if !Truthy(left) {
		return Bool(false), nil
	}
	return ev.Eval(args[1], env)
}

func (ev *Evaluator) evalOr(args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return Bool(false), nil
	}
	left, err := ev.Eval(args[0], env)
	if err != nil {
		return nil, err
	}
	if Truthy(left) {
		return Bool(true), nil
	}
	return ev.Eval(args[1], env)
}

func (ev *Evaluator) evalDo(args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("do: need two expressions")
	}
	if _, err := ev.Eval(args[0], env); err != nil {
		return nil, err
	}
	return ev.Eval(args[1], env)
}

func (ev *Evaluator) evalLambda(args []Value, env *Env) (Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("/.: need param and body")
	}
	param := string(args[0].(Sym))
	return &Lambda{Param: param, Body: args[1], Env: env}, nil
}

func (ev *Evaluator) evalLoad(args []Value, env *Env) (Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("load: need filename")
	}
	fnameVal, err := ev.Eval(args[0], env)
	if err != nil {
		return nil, err
	}
	fname := string(fnameVal.(Str))
	fullPath := filepath.Join(ev.currentDir(), fname)
	return ev.LoadFile(fullPath)
}

// ---------------------------------------------------------------------------
// Function application
// ---------------------------------------------------------------------------

func (ev *Evaluator) evalApplication(f Form, env *Env) (Value, error) {
	fn, err := ev.Eval(f[0], env)
	if err != nil {
		return nil, err
	}
	args := make([]Value, len(f)-1)
	for i := 1; i < len(f); i++ {
		args[i-1], err = ev.Eval(f[i], env)
		if err != nil {
			return nil, err
		}
	}
	return ev.Apply(fn, args)
}

// Apply calls a function value with arguments.
func (ev *Evaluator) Apply(fn Value, args []Value) (Value, error) {
	switch f := fn.(type) {
	case *Defun:
		return ev.applyDefun(f, args)
	case *Lambda:
		return ev.applyLambda(f, args)
	case *BuiltinFn:
		if f.Arity >= 0 && len(args) < f.Arity {
			return ev.partialApply(fn, args)
		}
		return f.Fn(args)
	case Sym:
		if g, ok := ev.globals[string(f)]; ok {
			return ev.Apply(g, args)
		}
		return nil, fmt.Errorf("undefined function: %s", string(f))
	default:
		return nil, fmt.Errorf("not a function: %s (%T)", PrintValue(fn), fn)
	}
}

func (ev *Evaluator) applyDefun(d *Defun, args []Value) (Value, error) {
	if len(args) < d.Arity {
		return ev.partialApply(d, args)
	}

	for _, c := range d.Cases {
		bindings := make(map[string]Value)
		if matchPatterns(c.Patterns, args, bindings) {
			env := NewEnv(nil)
			for k, v := range bindings {
				env.Set(k, v)
			}
			if c.Guard != nil {
				guardResult, err := ev.Eval(c.Guard, env)
				if err != nil {
					continue // guard evaluation failure → skip case
				}
				if !Truthy(guardResult) {
					continue
				}
			}
			return ev.Eval(c.Body, env)
		}
	}
	return nil, fmt.Errorf("pattern match failure in %s with args: %s", d.Name, fmtArgs(args))
}

func (ev *Evaluator) applyLambda(lam *Lambda, args []Value) (Value, error) {
	if len(args) == 0 {
		return lam, nil
	}
	env := NewEnv(lam.Env)
	env.Set(lam.Param, args[0])
	result, err := ev.Eval(lam.Body, env)
	if err != nil {
		return nil, err
	}
	if len(args) > 1 {
		return ev.Apply(result, args[1:])
	}
	return result, nil
}

func (ev *Evaluator) partialApply(fn Value, args []Value) (Value, error) {
	capturedFn := fn
	capturedArgs := make([]Value, len(args))
	copy(capturedArgs, args)

	remaining := 0
	switch f := fn.(type) {
	case *BuiltinFn:
		remaining = f.Arity - len(args)
	case *Defun:
		remaining = f.Arity - len(args)
	default:
		remaining = 1
	}

	return &BuiltinFn{
		Name:  "<partial>",
		Arity: remaining,
		Fn: func(moreArgs []Value) (Value, error) {
			all := append(capturedArgs, moreArgs...)
			return ev.Apply(capturedFn, all)
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Pattern matching
// ---------------------------------------------------------------------------

func matchPatterns(patterns, values []Value, bindings map[string]Value) bool {
	if len(patterns) != len(values) {
		return false
	}
	for i, p := range patterns {
		if !matchPattern(p, values[i], bindings) {
			return false
		}
	}
	return true
}

func matchPattern(pattern, value Value, bindings map[string]Value) bool {
	switch p := pattern.(type) {
	case nil:
		return value == nil

	case Sym:
		name := string(p)
		if name == "_" {
			return true
		}
		if isPatternVariable(name) {
			if existing, ok := bindings[name]; ok {
				return ValueEqual(existing, value)
			}
			bindings[name] = value
			return true
		}
		// Literal symbol
		vs, ok := value.(Sym)
		return ok && string(vs) == name

	case Str:
		vs, ok := value.(Str)
		return ok && string(vs) == string(p)

	case Num:
		vn, ok := value.(Num)
		return ok && float64(vn) == float64(p)

	case Bool:
		vb, ok := value.(Bool)
		return ok && bool(vb) == bool(p)

	case *Cons:
		vc, ok := value.(*Cons)
		if !ok {
			return false
		}
		return matchPattern(p.Car, vc.Car, bindings) &&
			matchPattern(p.Cdr, vc.Cdr, bindings)

	default:
		return false
	}
}

// isPatternVariable returns true for symbols that act as variables in patterns.
// Convention: uppercase first letter → variable; lowercase → literal symbol.
func isPatternVariable(name string) bool {
	if len(name) == 0 {
		return false
	}
	ch := name[0]
	return ch >= 'A' && ch <= 'Z'
}

func fmtArgs(args []Value) string {
	parts := make([]string, len(args))
	for i, a := range args {
		s := PrintValue(a)
		if len(s) > 80 {
			s = s[:77] + "..."
		}
		parts[i] = s
	}
	return strings.Join(parts, ", ")
}
