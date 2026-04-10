// Package ir builds the typed intermediate representation (World) from
// loaded YAML documents. This is the bridge between raw YAML and the
// type checker.
package ir

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/pyrex41/cross-validate-/pkg/loader"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Builder constructs a World from loaded documents.
type Builder struct {
	world *types.World
}

// NewBuilder creates a new IR builder.
func NewBuilder() *Builder {
	return &Builder{
		world: types.NewWorld(),
	}
}

// Build processes all loaded documents and returns the World.
func (b *Builder) Build(docs []loader.LoadedDocument) (*types.World, error) {
	for _, doc := range docs {
		category := loader.ClassifyDocument(doc)
		var err error
		switch category {
		case "crd":
			err = b.addCRD(doc)
		case "xrd":
			err = b.addXRD(doc)
		case "composition":
			err = b.addComposition(doc)
		case "function":
			err = b.addFunction(doc)
		case "provider":
			err = b.addProvider(doc)
		case "configuration":
			err = b.addConfiguration(doc)
		case "argo-application":
			err = b.addArgoApplication(doc)
		case "resource":
			err = b.addResource(doc)
		}
		if err != nil {
			return nil, fmt.Errorf("processing %s %s from %s:%d: %w",
				doc.Kind, getName(doc.Raw), doc.Source.File, doc.Source.Line, err)
		}
	}
	return b.world, nil
}

func (b *Builder) addCRD(doc loader.LoadedDocument) error {
	spec := getMap(doc.Raw, "spec")
	if spec == nil {
		return fmt.Errorf("CRD missing spec")
	}

	group, _ := spec["group"].(string)
	names := getMap(spec, "names")
	kind := ""
	if names != nil {
		kind, _ = names["kind"].(string)
	}
	scope, _ := spec["scope"].(string)

	crd := types.CRDInfo{
		Group:  group,
		Kind:   kind,
		Scope:  scope,
		Source: doc.Source,
	}

	versions := getSlice(spec, "versions")
	for _, v := range versions {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := vm["name"].(string)
		served, _ := vm["served"].(bool)
		storage, _ := vm["storage"].(bool)

		ver := types.CRDVersion{
			Name:    name,
			Served:  served,
			Storage: storage,
		}

		// Hash the schema for deduplication
		if schema := getMap(vm, "schema"); schema != nil {
			digest := hashSchema(schema)
			ver.SchemaDigest = digest
			b.world.Schemas[digest] = types.SchemaInfo{
				Digest: digest,
				Schema: schema,
			}
		}

		crd.Versions = append(crd.Versions, ver)
	}

	// Parse conversion info
	conv := getMap(spec, "conversion")
	if conv != nil {
		strategy, _ := conv["strategy"].(string)
		crd.Conversion.Strategy = strategy
		switch strategy {
		case "Webhook":
			crd.Conversion.CostClass = types.CostClassWebhook
			// Try to extract webhook service info
			if wh := getMap(conv, "webhook"); wh != nil {
				if cc := getMap(wh, "clientConfig"); cc != nil {
					if svc := getMap(cc, "service"); svc != nil {
						ns, _ := svc["namespace"].(string)
						name, _ := svc["name"].(string)
						crd.Conversion.WebhookService = ns + "/" + name
					}
				}
			}
		case "None", "":
			if len(crd.Versions) <= 1 {
				crd.Conversion.CostClass = types.CostClassNone
			} else {
				// Check if schemas are identical across versions
				allSame := true
				var firstDigest string
				for i, v := range crd.Versions {
					if i == 0 {
						firstDigest = v.SchemaDigest
					} else if v.SchemaDigest != firstDigest {
						allSame = false
						break
					}
				}
				if allSame {
					crd.Conversion.CostClass = types.CostClassIdentity
				} else {
					crd.Conversion.CostClass = types.CostClassStructural
				}
			}
		}
	} else {
		crd.Conversion.CostClass = types.CostClassNone
	}

	b.world.CRDs = append(b.world.CRDs, crd)
	return nil
}

func (b *Builder) addXRD(doc loader.LoadedDocument) error {
	spec := getMap(doc.Raw, "spec")
	if spec == nil {
		return fmt.Errorf("XRD missing spec")
	}

	group, _ := spec["group"].(string)
	names := getMap(spec, "names")
	kind := ""
	if names != nil {
		kind, _ = names["kind"].(string)
	}
	claimNames := getMap(spec, "claimNames")
	scope := "Cluster"
	if claimNames != nil {
		scope = "Namespaced"
	}

	xrd := types.CRDInfo{
		Group:      group,
		Kind:       kind,
		Scope:      scope,
		Source:     doc.Source,
		IsXRD:      true,
		APIVersion: doc.APIVersion,
	}

	versions := getSlice(spec, "versions")
	for _, v := range versions {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := vm["name"].(string)
		served, _ := vm["served"].(bool)
		referenceable, _ := vm["referenceable"].(bool)

		ver := types.CRDVersion{
			Name:          name,
			Served:        served,
			Storage:       referenceable, // In XRDs, referenceable ≈ storage
			Referenceable: referenceable,
		}

		if schema := getMap(vm, "schema"); schema != nil {
			digest := hashSchema(schema)
			ver.SchemaDigest = digest
			b.world.Schemas[digest] = types.SchemaInfo{
				Digest: digest,
				Schema: schema,
			}
		}

		xrd.Versions = append(xrd.Versions, ver)
	}

	b.world.XRDs = append(b.world.XRDs, xrd)
	return nil
}

func (b *Builder) addComposition(doc loader.LoadedDocument) error {
	spec := getMap(doc.Raw, "spec")
	if spec == nil {
		return fmt.Errorf("Composition missing spec")
	}

	metadata := getMap(doc.Raw, "metadata")
	name := ""
	if metadata != nil {
		name, _ = metadata["name"].(string)
	}

	comp := types.CompositionInfo{
		Name:   name,
		Source: doc.Source,
	}

	// Parse compositeTypeRef
	ctr := getMap(spec, "compositeTypeRef")
	if ctr != nil {
		apiVersion, _ := ctr["apiVersion"].(string)
		kind, _ := ctr["kind"].(string)
		comp.CompositeTypeRef = parseGVK(apiVersion, kind)
	}

	// Determine mode
	if pipeline := getSlice(spec, "pipeline"); pipeline != nil {
		comp.Mode = "Pipeline"
		for _, step := range pipeline {
			sm, ok := step.(map[string]interface{})
			if !ok {
				continue
			}
			ps := types.PipelineStep{}
			ps.Name, _ = sm["step"].(string)

			if fr := getMap(sm, "functionRef"); fr != nil {
				ps.FunctionRef, _ = fr["name"].(string)
			}

			if input := getMap(sm, "input"); input != nil {
				ps.InputAPIVersion, _ = input["apiVersion"].(string)
				ps.InputKind, _ = input["kind"].(string)
				digest := hashSchema(input)
				ps.InputDigest = digest
				b.world.Schemas[digest] = types.SchemaInfo{
					Digest: digest,
					Schema: input,
				}
			}
			comp.Pipeline = append(comp.Pipeline, ps)
		}
	} else if resources := getSlice(spec, "resources"); resources != nil {
		comp.Mode = "Resources"
		for _, res := range resources {
			rm, ok := res.(map[string]interface{})
			if !ok {
				continue
			}
			cr := types.ComposedResource{}
			cr.Name, _ = rm["name"].(string)

			if base := getMap(rm, "base"); base != nil {
				av, _ := base["apiVersion"].(string)
				k, _ := base["kind"].(string)
				cr.Base = types.ResourceBase{APIVersion: av, Kind: k}
			}

			if patches := getSlice(rm, "patches"); patches != nil {
				for _, p := range patches {
					pm, ok := p.(map[string]interface{})
					if !ok {
						continue
					}
					pi := types.PatchInfo{}
					pi.Type, _ = pm["type"].(string)
					pi.FromFieldPath, _ = pm["fromFieldPath"].(string)
					pi.ToFieldPath, _ = pm["toFieldPath"].(string)

					if transforms := getSlice(pm, "transforms"); transforms != nil {
						for _, t := range transforms {
							tm, ok := t.(map[string]interface{})
							if !ok {
								continue
							}
							ti := types.TransformInfo{}
							ti.Type, _ = tm["type"].(string)
							if conv := getMap(tm, "convert"); conv != nil {
								ti.Convert, _ = conv["toType"].(string)
							}
							pi.Transforms = append(pi.Transforms, ti)
						}
					}
					cr.Patches = append(cr.Patches, pi)
				}
			}
			comp.Resources = append(comp.Resources, cr)
		}
	}

	b.world.Compositions = append(b.world.Compositions, comp)
	return nil
}

func (b *Builder) addFunction(doc loader.LoadedDocument) error {
	spec := getMap(doc.Raw, "spec")
	metadata := getMap(doc.Raw, "metadata")

	name := ""
	if metadata != nil {
		name, _ = metadata["name"].(string)
	}

	fn := types.FunctionInfo{
		Name:   name,
		Source: doc.Source,
	}

	if spec != nil {
		pkg, _ := spec["package"].(string)
		fn.Package = pkg
	}

	// Infer input versions from well-known functions
	fn.InputVersions = inferFunctionInputVersions(name, fn.Package)

	b.world.Functions = append(b.world.Functions, fn)
	return nil
}

func (b *Builder) addProvider(doc loader.LoadedDocument) error {
	spec := getMap(doc.Raw, "spec")
	metadata := getMap(doc.Raw, "metadata")

	name := ""
	if metadata != nil {
		name, _ = metadata["name"].(string)
	}

	prov := types.ProviderInfo{
		Name:   name,
		Source: doc.Source,
	}

	if spec != nil {
		pkg, _ := spec["package"].(string)
		prov.Package = pkg
	}

	b.world.Providers = append(b.world.Providers, prov)
	return nil
}

func (b *Builder) addConfiguration(doc loader.LoadedDocument) error {
	spec := getMap(doc.Raw, "spec")
	metadata := getMap(doc.Raw, "metadata")

	name := ""
	if metadata != nil {
		name, _ = metadata["name"].(string)
	}

	cfg := types.ConfigurationInfo{
		Name:   name,
		Source: doc.Source,
	}

	if spec != nil {
		pkg, _ := spec["package"].(string)
		cfg.Package = pkg
	}

	b.world.Configurations = append(b.world.Configurations, cfg)
	return nil
}

func (b *Builder) addArgoApplication(doc loader.LoadedDocument) error {
	metadata := getMap(doc.Raw, "metadata")
	name := ""
	ns := ""
	if metadata != nil {
		name, _ = metadata["name"].(string)
		ns, _ = metadata["namespace"].(string)
	}

	app := types.ArgoApplication{
		Name:         name,
		Namespace:    ns,
		TrackingMode: "annotation", // default
		Source:       doc.Source,
	}

	// Check for tracking mode annotation
	if metadata != nil {
		annotations := getMap(metadata, "annotations")
		if annotations != nil {
			if tm, ok := annotations["argocd.argoproj.io/tracking-method"].(string); ok {
				app.TrackingMode = tm
			}
		}
	}

	b.world.ArgoApps = append(b.world.ArgoApps, app)
	return nil
}

func (b *Builder) addResource(doc loader.LoadedDocument) error {
	metadata := getMap(doc.Raw, "metadata")
	name := ""
	ns := ""
	annotations := map[string]string{}
	labels := map[string]string{}

	if metadata != nil {
		name, _ = metadata["name"].(string)
		ns, _ = metadata["namespace"].(string)

		if ann := getMap(metadata, "annotations"); ann != nil {
			for k, v := range ann {
				if vs, ok := v.(string); ok {
					annotations[k] = vs
				}
			}
		}
		if lbl := getMap(metadata, "labels"); lbl != nil {
			for k, v := range lbl {
				if vs, ok := v.(string); ok {
					labels[k] = vs
				}
			}
		}
	}

	res := types.ResourceInfo{
		APIVersion:  doc.APIVersion,
		Kind:        doc.Kind,
		Name:        name,
		Namespace:   ns,
		Annotations: annotations,
		Labels:      labels,
		Source:      doc.Source,
		Raw:         doc.Raw,
	}

	b.world.Resources = append(b.world.Resources, res)
	return nil
}

// Helper functions

func getMap(m map[string]interface{}, key string) map[string]interface{} {
	v, ok := m[key]
	if !ok {
		return nil
	}
	vm, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	return vm
}

func getSlice(m map[string]interface{}, key string) []interface{} {
	v, ok := m[key]
	if !ok {
		return nil
	}
	vs, ok := v.([]interface{})
	if !ok {
		return nil
	}
	return vs
}

func getName(raw map[string]interface{}) string {
	metadata := getMap(raw, "metadata")
	if metadata == nil {
		return "<unknown>"
	}
	name, _ := metadata["name"].(string)
	if name == "" {
		return "<unknown>"
	}
	return name
}

func hashSchema(schema map[string]interface{}) string {
	data, _ := json.Marshal(schema)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:16]) // truncated for readability
}

func parseGVK(apiVersion, kind string) types.GVK {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 2 {
		return types.GVK{Group: parts[0], Version: parts[1], Kind: kind}
	}
	return types.GVK{Group: "", Version: apiVersion, Kind: kind}
}

// inferFunctionInputVersions returns known input versions for well-known functions.
func inferFunctionInputVersions(name, pkg string) []string {
	// Map of well-known functions to their accepted input versions
	knownFunctions := map[string][]string{
		"function-patch-and-transform": {"pt.fn.crossplane.io/v1beta1"},
		"function-kcl":                 {"krm.kcl.dev/v1alpha1"},
		"function-go-templating":       {"gotemplating.fn.crossplane.io/v1beta1"},
		"function-auto-ready":          {}, // no input
		"function-cel-filter":          {"celfilter.fn.crossplane.io/v1beta1"},
		"function-status-transformer":  {"statustransformer.fn.crossplane.io/v1beta1"},
		"function-environment-configs":  {"environmentconfigs.fn.crossplane.io/v1beta1"},
		"function-extra-resources":      {"extraresources.fn.crossplane.io/v1beta1"},
		"function-sequencer":           {"sequencer.fn.crossplane.io/v1beta1"},
	}

	for knownName, versions := range knownFunctions {
		if name == knownName || strings.Contains(pkg, knownName) {
			return versions
		}
	}
	return nil // unknown function
}

// ParseSyncWave extracts the sync wave integer from a resource's annotations.
func ParseSyncWave(annotations map[string]string) int {
	wave, ok := annotations["argocd.argoproj.io/sync-wave"]
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(wave)
	if err != nil {
		return 0
	}
	return n
}
