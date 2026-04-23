package plan

import (
	"reflect"
	"sort"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// indexedResource is the internal view Diff operates on. ResourceInfo has a
// Raw map, ArgoApplication doesn't — we synthesize an equivalent "virtual
// raw" for Applications so R26 can inspect finalizers / syncPolicy uniformly.
type indexedResource struct {
	Source types.SourceLocation
	Raw    map[string]interface{}
}

// Diff computes the resource-identity delta between two Worlds. Pure function;
// deterministic (results sorted by (AppName, APIVersion, Kind, Namespace,
// Name)) so the output is stable across runs for the same input.
//
// Identity key: (APIVersion, Kind, Namespace, Name, AppName). Two resources
// in different Applications with the same GVK+name are treated as distinct
// — they live at different ownership scopes, and a rule like R26 cares about
// per-app deletion, not global deletion.
//
// ArgoApplications are included alongside ResourceInfo in the delta so R26
// can flag cascade-risk on removed Applications (kind=Application,
// group=argoproj.io). Their "raw" payload is synthesized from typed fields
// on ArgoApplication — full fidelity isn't needed, only the fields R26
// consults (finalizers, syncPolicy.preserveResourcesOnDeletion, annotations).
//
// Modified = same identity on both sides, different content. For
// ResourceInfo we deep-equal on Raw. For ArgoApplication we rebuild the
// virtual raw on both sides and deep-equal those — covers the fields we
// care about and ignores upstream parser noise.
func Diff(base, head *types.World) ResourceDelta {
	baseMap := indexAll(base)
	headMap := indexAll(head)

	var added, removed, modified []ResourceChange
	for id, baseRes := range baseMap {
		headRes, present := headMap[id]
		if !present {
			removed = append(removed, ResourceChange{
				Identity:   id,
				BaseSource: baseRes.Source,
				BaseRaw:    baseRes.Raw,
			})
			continue
		}
		if !reflect.DeepEqual(baseRes.Raw, headRes.Raw) {
			modified = append(modified, ResourceChange{
				Identity:   id,
				BaseSource: baseRes.Source,
				HeadSource: headRes.Source,
				BaseRaw:    baseRes.Raw,
				HeadRaw:    headRes.Raw,
			})
		}
	}
	for id, headRes := range headMap {
		if _, present := baseMap[id]; !present {
			added = append(added, ResourceChange{
				Identity:   id,
				HeadSource: headRes.Source,
				HeadRaw:    headRes.Raw,
			})
		}
	}

	sortChanges(added)
	sortChanges(removed)
	sortChanges(modified)
	return ResourceDelta{Added: added, Removed: removed, Modified: modified}
}

// indexAll unions ResourceInfo + ArgoApplication rows by ResourceIdentity.
func indexAll(w *types.World) map[ResourceIdentity]indexedResource {
	out := make(map[ResourceIdentity]indexedResource, len(w.Resources)+len(w.ArgoApps))
	for id, res := range indexByIdentity(w) {
		out[id] = indexedResource{Source: res.Source, Raw: res.Raw}
	}
	for i := range w.ArgoApps {
		app := &w.ArgoApps[i]
		id := ResourceIdentity{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "Application",
			Namespace:  app.Namespace,
			Name:       app.Name,
			// AppName intentionally empty — the Application IS the app.
		}
		out[id] = indexedResource{
			Source: app.Source,
			Raw:    argoAppVirtualRaw(app),
		}
	}
	return out
}

// argoAppVirtualRaw rebuilds the subset of metadata / spec fields R26 reads
// into a map shape matching the original YAML. Deterministic: nil slices
// collapse to absent keys, so deep-equal works.
func argoAppVirtualRaw(app *types.ArgoApplication) map[string]interface{} {
	meta := map[string]interface{}{
		"name": app.Name,
	}
	if app.Namespace != "" {
		meta["namespace"] = app.Namespace
	}
	if len(app.Finalizers) > 0 {
		fins := make([]interface{}, 0, len(app.Finalizers))
		for _, f := range app.Finalizers {
			fins = append(fins, f)
		}
		meta["finalizers"] = fins
	}
	if len(app.Annotations) > 0 {
		ann := make(map[string]interface{}, len(app.Annotations))
		for k, v := range app.Annotations {
			ann[k] = v
		}
		meta["annotations"] = ann
	}

	sp := map[string]interface{}{}
	if app.SyncPolicy.PreserveResourcesOnDeletion {
		sp["preserveResourcesOnDeletion"] = true
	}
	spec := map[string]interface{}{}
	if len(sp) > 0 {
		spec["syncPolicy"] = sp
	}

	return map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata":   meta,
		"spec":       spec,
	}
}

// indexByIdentity returns a snapshot of w.Resources keyed by ResourceIdentity.
// The Resource's APIVersion/Kind/Namespace/Name form four components; AppName
// is filled from ResourceInfo.OwningApp (the expanded-app field populated by
// the AppSet expander). When multiple resources collide on the same identity,
// the last one wins — this mirrors how a Kubernetes API-server would resolve
// the collision, and keeps the diff stable.
func indexByIdentity(w *types.World) map[ResourceIdentity]*types.ResourceInfo {
	out := make(map[ResourceIdentity]*types.ResourceInfo, len(w.Resources))
	for i := range w.Resources {
		res := &w.Resources[i]
		id := ResourceIdentity{
			APIVersion: res.APIVersion,
			Kind:       res.Kind,
			Namespace:  res.Namespace,
			Name:       res.Name,
			AppName:    res.OwningApp,
		}
		out[id] = res
	}
	return out
}

// sortChanges orders a slice of ResourceChange by identity for deterministic
// output. AppName is the outer key because R26's narrative ("App foo removes
// resource X") is per-app.
func sortChanges(changes []ResourceChange) {
	sort.Slice(changes, func(i, j int) bool {
		a, b := changes[i].Identity, changes[j].Identity
		if a.AppName != b.AppName {
			return a.AppName < b.AppName
		}
		if a.APIVersion != b.APIVersion {
			return a.APIVersion < b.APIVersion
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
}
