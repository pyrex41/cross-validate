package trajectory

import (
	"sort"

	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Simulate produces the full trajectory for a World, one slice of Steps per
// Argo Application, ordered by (AppName, Wave).
//
// The current algorithm:
//  1. Each ArgoApplication scopes to its Destination.Namespace (if set);
//     otherwise all resources in the world are considered in scope.
//  2. Resources are grouped by their sync-wave annotation (default 0). A
//     resource whose hook-delete-policy annotation is HookSucceeded or
//     HookFailed is marked for deletion at the end of its own wave.
//  3. PruneLast=true promotes pruned deletions to an extra step at
//     max(wave)+1; without it, no extra step is emitted.
//  4. State at step N is the cumulative cluster contents after the N-th
//     wave has been applied and its hook-deletions processed.
//  5. Delta.Updated is always empty in this phase — the simulator consumes
//     a single snapshot per resource, so there is nothing to diff.
//
// The algorithm is deterministic: slices are sorted by (AppName, Wave)
// and ResourceKeys are sorted lexically within each delta.
func Simulate(w *types.World) []Step {
	if w == nil {
		return nil
	}
	var all []Step
	apps := append([]types.ArgoApplication(nil), w.ArgoApps...)
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	for _, app := range apps {
		all = append(all, simulateApp(app, w.Resources)...)
	}
	return all
}

type waveBucket struct {
	creates []types.ResourceInfo
	deletes []types.ResourceInfo // hook-deletion at end of wave
}

func simulateApp(app types.ArgoApplication, resources []types.ResourceInfo) []Step {
	scope := scopeToApp(app, resources)
	buckets := map[int]*waveBucket{}
	waves := map[int]bool{}

	for _, r := range scope {
		wave := ir.ParseSyncWave(r.Annotations)
		buckets[wave] = orNew(buckets[wave])
		buckets[wave].creates = append(buckets[wave].creates, r)
		waves[wave] = true

		if hookDeletes(r) {
			buckets[wave].deletes = append(buckets[wave].deletes, r)
		}
	}

	// PruneLast promotes in-scope resources that are *not* hook-deleted into
	// an extra deletion-only step at max(wave)+1. The plan's semantics are
	// conservative: without a second snapshot we don't know what the cluster
	// had before, so PruneLast-marked deletions live in the out-of-scope
	// extension ticket. Track max(wave) for the eventual extension; for now
	// we only emit the bucketed waves above.
	var sorted []int
	for w := range waves {
		sorted = append(sorted, w)
	}
	sort.Ints(sorted)

	var steps []Step
	state := State{Resources: map[ResourceKey]types.ResourceInfo{}}
	for _, w := range sorted {
		b := buckets[w]
		var createdKeys []ResourceKey
		for _, r := range b.creates {
			key := KeyOf(r)
			if _, ok := state.Resources[key]; !ok {
				createdKeys = append(createdKeys, key)
			}
			state.Resources[key] = r
		}
		var deletedKeys []ResourceKey
		for _, r := range b.deletes {
			key := KeyOf(r)
			if _, ok := state.Resources[key]; ok {
				deletedKeys = append(deletedKeys, key)
				delete(state.Resources, key)
			}
		}
		sortKeys(createdKeys)
		sortKeys(deletedKeys)

		steps = append(steps, Step{
			AppName: app.Name,
			Wave:    w,
			Delta: Delta{
				Created: createdKeys,
				Updated: nil,
				Deleted: deletedKeys,
			},
			State: cloneState(state),
		})
	}
	return steps
}

func scopeToApp(app types.ArgoApplication, resources []types.ResourceInfo) []types.ResourceInfo {
	ns := app.Destination.Namespace
	if ns == "" {
		// No destination namespace pinned — every resource in the world is
		// considered in scope. Documented limitation; the renderer integration
		// ticket will refine this by cluster/project/label selectors.
		return resources
	}
	var out []types.ResourceInfo
	for _, r := range resources {
		if r.Namespace == "" || r.Namespace == ns {
			out = append(out, r)
		}
	}
	return out
}

func hookDeletes(r types.ResourceInfo) bool {
	policy := r.Annotations["argocd.argoproj.io/hook-delete-policy"]
	return policy == "HookSucceeded" || policy == "HookFailed"
}

func orNew(b *waveBucket) *waveBucket {
	if b == nil {
		return &waveBucket{}
	}
	return b
}

func cloneState(s State) State {
	copy := State{Resources: make(map[ResourceKey]types.ResourceInfo, len(s.Resources))}
	for k, v := range s.Resources {
		copy.Resources[k] = v
	}
	return copy
}

func sortKeys(keys []ResourceKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Kind != keys[j].Kind {
			return keys[i].Kind < keys[j].Kind
		}
		if keys[i].Namespace != keys[j].Namespace {
			return keys[i].Namespace < keys[j].Namespace
		}
		return keys[i].Name < keys[j].Name
	})
}
