package shenfull

import (
	"fmt"

	"github.com/tiancaiamao/shen-go/kl"
)

// ns2_1set and try_1catch are package-level globals referenced throughout
// the vendored Shen bootstrap files (originally defined in shen-go's
// cmd/shen/main.go). They must be initialized before any *Main loader runs.
var (
	ns2_1set   kl.Obj
	try_1catch kl.Obj
)

// Init bootstraps the Shen language on top of kl. It must be called exactly
// once per ControlFlow before any Shen code is loaded or called. Mirrors
// shen-go's cmd/shen/main.go: regist all 16 AOT modules, install the load
// cache + native reader hooks, then run (shen.initialise) to populate the
// runtime tables and bind standard-library functions like list/cons/append
// that kernel rule type signatures reference at define time.
func Init(e *kl.ControlFlow) error {
	ns2_1set = kl.PrimFunc(kl.MakeSymbol("defun"))
	try_1catch = kl.PrimFunc(kl.MakeSymbol("try-catch"))

	loaders := []struct {
		name string
		fn   kl.Obj
	}{
		{"TopLevelMain", TopLevelMain},
		{"CoreMain", CoreMain},
		{"SysMain", SysMain},
		{"SequentMain", SequentMain},
		{"YaccMain", YaccMain},
		{"ReaderMain", ReaderMain},
		{"PrologMain", PrologMain},
		{"TrackMain", TrackMain},
		{"LoadMain", LoadMain},
		{"WriterMain", WriterMain},
		{"MacrosMain", MacrosMain},
		{"DeclarationsMain", DeclarationsMain},
		{"TStarMain", TStarMain},
		{"TypesMain", TypesMain},
		{"DictMain", DictMain},
		{"InitMain", InitMain},
	}

	for _, l := range loaders {
		res := kl.Call(e, l.fn)
		if kl.IsError(res) {
			return fmt.Errorf("shenfull.Init: %s returned error: %s", l.name, kl.ObjString(res))
		}
	}

	// installLoadCache hooks the parsed-form cache so repeated `(load X)`
	// reuses the prior parse. Defined in load_cache.go; matches shen-go
	// cmd/shen/main.go's regist tail.
	installLoadCache()

	// Final bootstrap step: run (shen.initialise) to set up runtime tables
	// (arity, environment, dispatch) and bind stdlib functions.
	res := kl.Eval(e, kl.Cons(kl.MakeSymbol("shen.initialise"), kl.Nil))
	if kl.IsError(res) {
		return fmt.Errorf("shenfull.Init: shen.initialise returned error: %s", kl.ObjString(res))
	}
	return nil
}
