package obligation

import (
	"fmt"
	"sync"
)

// Registry holds all known generators, keyed by name.
// The registry is the single source of truth for what xpc checks.
type Registry struct {
	generators []Generator
	byName     map[string]Generator
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		byName: make(map[string]Generator),
	}
}

// Register adds a generator to the registry.
// Panics if a generator with the same name is already registered.
func (r *Registry) Register(g Generator) {
	if _, exists := r.byName[g.Name()]; exists {
		panic(fmt.Sprintf("obligation: duplicate generator name %q", g.Name()))
	}
	r.generators = append(r.generators, g)
	r.byName[g.Name()] = g
}

// All returns all registered generators in registration order.
func (r *Registry) All() []Generator {
	return r.generators
}

// Get returns a generator by name, or nil if not found.
func (r *Registry) Get(name string) Generator {
	return r.byName[name]
}

// Categories returns the set of categories that have at least one generator.
func (r *Registry) Categories() []Category {
	seen := make(map[Category]bool)
	var cats []Category
	for _, g := range r.generators {
		if !seen[g.Category()] {
			seen[g.Category()] = true
			cats = append(cats, g.Category())
		}
	}
	return cats
}

var (
	defaultOnce     sync.Once
	defaultRegistry *Registry
)

// RegisterDefault adds a generator to the default registry.
// Called by generator packages in their init() functions.
func RegisterDefault(g Generator) {
	initDefault()
	defaultRegistry.Register(g)
}

func initDefault() {
	defaultOnce.Do(func() {
		defaultRegistry = NewRegistry()
	})
}

// DefaultRegistry returns the registry with all built-in generators.
// During Phase 0, this contains only the pilot generator (comp-xrd-ref).
// Phase 1 will absorb R1-R11 here.
func DefaultRegistry() *Registry {
	initDefault()
	return defaultRegistry
}
