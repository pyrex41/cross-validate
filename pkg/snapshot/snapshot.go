// Package snapshot captures and manages cluster type environment snapshots.
// A snapshot is a signed, content-addressed artifact that captures the type
// environment of a cluster at a moment in time.
package snapshot

import (
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Snapshot captures the type environment of a cluster at a moment in time.
type Snapshot struct {
	// Version of the snapshot format.
	Version int `json:"version"`

	// Digest is the content-addressed hash of this snapshot.
	Digest string `json:"digest"`

	// Timestamp when the snapshot was taken.
	Timestamp time.Time `json:"timestamp"`

	// ClusterName identifies the cluster.
	ClusterName string `json:"clusterName"`

	// KubernetesVersion of the cluster.
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`

	// CRDs installed in the cluster with full schema, version list, storage version,
	// and conversion strategy classification.
	CRDs []types.CRDInfo `json:"crds"`

	// XRDs installed in the cluster.
	XRDs []types.CRDInfo `json:"xrds"`

	// Providers installed in the cluster.
	Providers []ProviderStatus `json:"providers"`

	// Functions installed in the cluster.
	Functions []FunctionStatus `json:"functions"`

	// Configurations installed in the cluster.
	Configurations []types.ConfigurationInfo `json:"configurations"`

	// Compositions installed in the cluster.
	Compositions []types.CompositionInfo `json:"compositions"`

	// ArgoTrackingMode is the Argo CD resource tracking mode (annotation or label).
	ArgoTrackingMode string `json:"argoTrackingMode,omitempty"`

	// Schemas are content-addressed schemas from CRDs.
	Schemas map[string]types.SchemaInfo `json:"schemas,omitempty"`

	// SigningIdentity that signed this snapshot.
	SigningIdentity string `json:"signingIdentity,omitempty"`

	// Signature over the snapshot content.
	Signature string `json:"signature,omitempty"`
}

// ProviderStatus is a Provider with its health status.
type ProviderStatus struct {
	types.ProviderInfo
	Version string `json:"version,omitempty"`
	Healthy bool   `json:"healthy"`
}

// FunctionStatus is a Function with its health status.
type FunctionStatus struct {
	types.FunctionInfo
	Version string `json:"version,omitempty"`
	Healthy bool   `json:"healthy"`
}

// New creates a new empty snapshot for the given cluster.
func New(clusterName string) *Snapshot {
	return &Snapshot{
		Version:     1,
		ClusterName: clusterName,
		Timestamp:   time.Now().UTC(),
		Schemas:     make(map[string]types.SchemaInfo),
	}
}

// ComputeDigest computes and sets the content-addressed digest of this snapshot.
// The digest is computed over all content fields (not the digest, signature, or timestamp).
func (s *Snapshot) ComputeDigest() string {
	h := sha256.New()

	// Deterministic serialization of content fields
	h.Write([]byte(fmt.Sprintf("v%d", s.Version)))
	h.Write([]byte(s.ClusterName))
	h.Write([]byte(s.KubernetesVersion))

	crdKey := func(c types.CRDInfo) string { return c.Group + "/" + c.Kind }
	crdByKey := func(a, b types.CRDInfo) int { return cmp.Compare(crdKey(a), crdKey(b)) }

	sortedCRDs := slices.Clone(s.CRDs)
	slices.SortFunc(sortedCRDs, crdByKey)
	for _, crd := range sortedCRDs {
		data, _ := json.Marshal(crd)
		h.Write(data)
	}

	sortedXRDs := slices.Clone(s.XRDs)
	slices.SortFunc(sortedXRDs, crdByKey)
	for _, xrd := range sortedXRDs {
		data, _ := json.Marshal(xrd)
		h.Write(data)
	}

	sortedProviders := slices.Clone(s.Providers)
	slices.SortFunc(sortedProviders, func(a, b ProviderStatus) int { return cmp.Compare(a.Name, b.Name) })
	for _, p := range sortedProviders {
		data, _ := json.Marshal(p)
		h.Write(data)
	}

	sortedFunctions := slices.Clone(s.Functions)
	slices.SortFunc(sortedFunctions, func(a, b FunctionStatus) int { return cmp.Compare(a.Name, b.Name) })
	for _, f := range sortedFunctions {
		data, _ := json.Marshal(f)
		h.Write(data)
	}

	sortedComps := slices.Clone(s.Compositions)
	slices.SortFunc(sortedComps, func(a, b types.CompositionInfo) int { return cmp.Compare(a.Name, b.Name) })
	for _, c := range sortedComps {
		data, _ := json.Marshal(c)
		h.Write(data)
	}

	h.Write([]byte(s.ArgoTrackingMode))

	s.Digest = fmt.Sprintf("sha256:%x", h.Sum(nil))
	return s.Digest
}

// Save writes the snapshot to a file.
func (s *Snapshot) Save(path string) error {
	if s.Digest == "" {
		s.ComputeDigest()
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling snapshot: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// Load reads a snapshot from a file.
func Load(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading snapshot: %w", err)
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshaling snapshot: %w", err)
	}
	return &s, nil
}

// Verify checks that the snapshot's digest matches its content.
func (s *Snapshot) Verify() bool {
	saved := s.Digest
	s.ComputeDigest()
	return s.Digest == saved
}

// Diff produces a human-readable diff between two snapshots.
func Diff(a, b *Snapshot) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Snapshot diff: %s vs %s\n", a.Digest[:20], b.Digest[:20]))
	sb.WriteString(fmt.Sprintf("  Cluster: %s → %s\n", a.ClusterName, b.ClusterName))
	sb.WriteString(fmt.Sprintf("  Time: %s → %s\n",
		a.Timestamp.Format(time.RFC3339), b.Timestamp.Format(time.RFC3339)))

	if a.KubernetesVersion != b.KubernetesVersion {
		sb.WriteString(fmt.Sprintf("  K8s version: %s → %s\n", a.KubernetesVersion, b.KubernetesVersion))
	}

	// CRD diffs
	aCRDs := make(map[string]*types.CRDInfo)
	for i := range a.CRDs {
		key := a.CRDs[i].Group + "/" + a.CRDs[i].Kind
		aCRDs[key] = &a.CRDs[i]
	}
	bCRDs := make(map[string]*types.CRDInfo)
	for i := range b.CRDs {
		key := b.CRDs[i].Group + "/" + b.CRDs[i].Kind
		bCRDs[key] = &b.CRDs[i]
	}

	for key, bCRD := range bCRDs {
		aCRD, ok := aCRDs[key]
		if !ok {
			sb.WriteString(fmt.Sprintf("  + CRD %s\n", key))
			continue
		}
		if aCRD.StorageVersion() != bCRD.StorageVersion() {
			sb.WriteString(fmt.Sprintf("  ~ CRD %s storage: %s → %s\n",
				key, aCRD.StorageVersion(), bCRD.StorageVersion()))
		}
		if string(aCRD.Conversion.CostClass) != string(bCRD.Conversion.CostClass) {
			sb.WriteString(fmt.Sprintf("  ~ CRD %s conversion: %s → %s\n",
				key, aCRD.Conversion.CostClass, bCRD.Conversion.CostClass))
		}
	}
	for key := range aCRDs {
		if _, ok := bCRDs[key]; !ok {
			sb.WriteString(fmt.Sprintf("  - CRD %s\n", key))
		}
	}

	// Provider diffs
	aProvs := make(map[string]*ProviderStatus)
	for i := range a.Providers {
		aProvs[a.Providers[i].Name] = &a.Providers[i]
	}
	bProvs := make(map[string]*ProviderStatus)
	for i := range b.Providers {
		bProvs[b.Providers[i].Name] = &b.Providers[i]
	}

	for name, bProv := range bProvs {
		aProv, ok := aProvs[name]
		if !ok {
			sb.WriteString(fmt.Sprintf("  + Provider %s (%s)\n", name, bProv.Package))
			continue
		}
		if aProv.Package != bProv.Package {
			sb.WriteString(fmt.Sprintf("  ~ Provider %s: %s → %s\n", name, aProv.Package, bProv.Package))
		}
		if aProv.Healthy != bProv.Healthy {
			sb.WriteString(fmt.Sprintf("  ~ Provider %s healthy: %t → %t\n", name, aProv.Healthy, bProv.Healthy))
		}
	}
	for name := range aProvs {
		if _, ok := bProvs[name]; !ok {
			sb.WriteString(fmt.Sprintf("  - Provider %s\n", name))
		}
	}

	// Function diffs
	aFns := make(map[string]*FunctionStatus)
	for i := range a.Functions {
		aFns[a.Functions[i].Name] = &a.Functions[i]
	}
	bFns := make(map[string]*FunctionStatus)
	for i := range b.Functions {
		bFns[b.Functions[i].Name] = &b.Functions[i]
	}

	for name, bFn := range bFns {
		aFn, ok := aFns[name]
		if !ok {
			sb.WriteString(fmt.Sprintf("  + Function %s (%s)\n", name, bFn.Package))
			continue
		}
		if aFn.Package != bFn.Package {
			sb.WriteString(fmt.Sprintf("  ~ Function %s: %s → %s\n", name, aFn.Package, bFn.Package))
		}
	}
	for name := range aFns {
		if _, ok := bFns[name]; !ok {
			sb.WriteString(fmt.Sprintf("  - Function %s\n", name))
		}
	}

	if a.ArgoTrackingMode != b.ArgoTrackingMode {
		sb.WriteString(fmt.Sprintf("  Argo tracking: %s → %s\n", a.ArgoTrackingMode, b.ArgoTrackingMode))
	}

	return sb.String()
}

// FromWorld creates a snapshot from a World (for offline/filesystem-based snapshots).
func FromWorld(w *types.World, clusterName string) *Snapshot {
	s := New(clusterName)
	s.CRDs = w.CRDs
	s.XRDs = w.XRDs
	s.Compositions = w.Compositions
	s.Schemas = w.Schemas

	for _, p := range w.Providers {
		s.Providers = append(s.Providers, ProviderStatus{
			ProviderInfo: p,
			Healthy:      true, // assume healthy from manifests
		})
	}

	for _, f := range w.Functions {
		s.Functions = append(s.Functions, FunctionStatus{
			FunctionInfo: f,
			Healthy:      true, // assume healthy from manifests
		})
	}

	s.Configurations = w.Configurations

	if len(w.ArgoApps) > 0 {
		s.ArgoTrackingMode = w.ArgoApps[0].TrackingMode
	}

	s.ComputeDigest()
	return s
}

// ToWorld converts a snapshot back to a World for type checking.
func (s *Snapshot) ToWorld() *types.World {
	w := types.NewWorld()
	w.CRDs = s.CRDs
	w.XRDs = s.XRDs
	w.Compositions = s.Compositions
	w.Schemas = s.Schemas

	for _, p := range s.Providers {
		w.Providers = append(w.Providers, p.ProviderInfo)
	}

	for _, f := range s.Functions {
		w.Functions = append(w.Functions, f.FunctionInfo)
	}

	w.Configurations = s.Configurations
	return w
}

// IsStale returns true if the snapshot is older than the given TTL.
func (s *Snapshot) IsStale(ttl time.Duration) bool {
	return time.Since(s.Timestamp) > ttl
}

// DefaultTTL is the default snapshot staleness TTL (15 minutes).
const DefaultTTL = 15 * time.Minute
