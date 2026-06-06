module github.com/pyrex41/cross-validate-

go 1.25

require (
	github.com/tiancaiamao/shen-go v0.0.0-20251114030759-7a6a67ac131d
	gopkg.in/yaml.v3 v3.0.1
)

// shen-go v1.2.0 ships kernel 41.1 + a bytecode VM compiler for KL
// (~15x tak, ~60x tight tail loops). The native Shen reader and load
// cache that keep xpc cold-check fast live in internal/shenfull here,
// so they are unaffected by the runtime version. The fork's VM work is
// pending upstream (tiancaiamao/shen-go PRs #50/#51); revert to the
// upstream module path once it lands there.
replace github.com/tiancaiamao/shen-go => github.com/pyrex41/shen-go v1.2.0
