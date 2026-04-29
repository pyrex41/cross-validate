module github.com/pyrex41/cross-validate-

go 1.25

require (
	github.com/tiancaiamao/shen-go v0.0.0-20251114030759-7a6a67ac131d
	gopkg.in/yaml.v3 v3.0.1
)

// shen-go v1.1.1 ships kernel 41.1 + a Go-native Shen reader + load cache
// that drop xpc cold-check from ~2.5s to ~0.7s. The fix lives on the
// pyrex41/shen-go fork (upstream tiancaiamao/shen-go hasn't taken the
// patch yet); revert to the upstream module path once they do.
replace github.com/tiancaiamao/shen-go => github.com/pyrex41/shen-go v1.1.1
