// Package trajectory simulates an Argo CD sync trajectory step-by-step.
//
// The simulator does NOT execute renderers. It operates on the resources
// already present in the World. Multi-source / Helm / Kustomize Applications
// see their Sources field reflected, but no actual templating happens.
package trajectory

import "github.com/pyrex41/cross-validate-/pkg/types"

// Step is a snapshot of the simulated cluster state at one wave.
type Step struct {
	AppName string
	Wave    int
	Delta   Delta
	State   State
}

// Delta is the set of resource keys that changed in a given step.
type Delta struct {
	Created []ResourceKey
	Updated []ResourceKey
	Deleted []ResourceKey
}

// State is the synthesized cluster contents AT a step.
type State struct {
	Resources map[ResourceKey]types.ResourceInfo
}

// ResourceKey is the canonical (kind, namespace, name) tuple used as a
// stable handle for resources across steps.
type ResourceKey struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

// KeyOf derives a ResourceKey from a ResourceInfo.
func KeyOf(r types.ResourceInfo) ResourceKey {
	return ResourceKey{
		APIVersion: r.APIVersion,
		Kind:       r.Kind,
		Namespace:  r.Namespace,
		Name:       r.Name,
	}
}
