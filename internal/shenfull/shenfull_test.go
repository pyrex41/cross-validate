package shenfull

import (
	"testing"

	"github.com/tiancaiamao/shen-go/kl"
)

func TestBootstrap(t *testing.T) {
	var e kl.ControlFlow
	if err := Init(&e); err != nil {
		t.Fatal(err)
	}
	// Evaluate (+ 1 2) via Shen.
	res := kl.Eval(&e, kl.Cons(kl.MakeSymbol("+"),
		kl.Cons(kl.MakeInteger(1),
			kl.Cons(kl.MakeInteger(2), kl.Nil))))
	if kl.IsError(res) {
		t.Fatalf("(+ 1 2) returned error: %s", kl.ObjString(res))
	}
	if got := kl.GetInteger(res); got != 3 {
		t.Fatalf("(+ 1 2) = %d, want 3 (obj: %s)", got, kl.ObjString(res))
	}
}
