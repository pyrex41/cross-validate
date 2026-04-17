package shenfull

import (
	"fmt"

	"github.com/tiancaiamao/shen-go/kl"
)

// ns2_1set and try_1catch are package-level globals referenced throughout the
// vendored Shen bootstrap files (originally defined in shen-go's cmd/shen/main.go).
// They must be initialized before any *Main loader runs.
var (
	ns2_1set   kl.Obj
	try_1catch kl.Obj
)

// Init bootstraps the Shen language on top of kl. It must be called exactly
// once per ControlFlow before any Shen code is loaded or called. Semantics
// mirror shen-go's cmd/shen/main.go `regist` sequence, but errors are surfaced
// to the caller rather than printed.
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
	}

	for _, l := range loaders {
		res := kl.Call(e, l.fn)
		if kl.IsError(res) {
			return fmt.Errorf("shenfull.Init: %s returned error: %s", l.name, kl.ObjString(res))
		}
	}
	return nil
}
