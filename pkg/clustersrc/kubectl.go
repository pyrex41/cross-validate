// Package clustersrc captures a live-cluster snapshot by shelling out to
// kubectl. It is the cluster-backed counterpart to the filesystem snapshot
// path in cmd/xpc (runSnapshot): rather than reading manifests off disk, it
// runs `kubectl get ... -o yaml` for the Crossplane/Argo/managed-resource
// kinds, parses the streams through the existing loader, builds a World, and
// hands it to snapshot.FromWorldWithOptions.
//
// kubectl was chosen over client-go deliberately: it adds zero Go
// dependencies (go.mod has no Kubernetes client libs) and matches xpc's
// existing helm/kustomize/crossplane shell-out philosophy. The exec /
// binary-resolution / timeout shape here is a direct mirror of
// pkg/renderer/composition.go.
package clustersrc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/loader"
	"github.com/pyrex41/cross-validate-/pkg/snapshot"
)

// CaptureTimeout caps each kubectl invocation. A single `kubectl get -A`
// against a large managed-resource set can be slow on a cold API server, so
// this is more generous than the 30s render ceiling. Per-call, not total.
const CaptureTimeout = 60 * time.Second

// ProbeTimeout caps the `kubectl version` reachability probe.
const ProbeTimeout = 10 * time.Second

// ErrKubectlAbsent is returned when the kubectl binary cannot be found on
// PATH or at the configured KubectlBin path. Callers MUST translate this into
// a hard error — there is NO silent fallback to filesystem capture, because a
// snapshot that silently came from disk instead of the cluster would corrupt
// every downstream git-vs-reality comparison.
var ErrKubectlAbsent = errors.New("clustersrc: kubectl binary absent")

// providerGroupSuffixes selects which CRD groups are treated as
// managed-resource (and Crossplane composite/claim) kinds worth capturing.
// Discovered dynamically from the cluster's CRD list rather than hardcoding
// individual kinds: fg-manifold alone spans hundreds of distinct GVKs across
// these groups, and the set drifts as providers are upgraded. Matching by
// group suffix keeps coverage explicit and stable.
//
// Verified against /Users/reuben/fg/fg-manifold/deploy: the live groups are
// *.aws.upbound.io (ec2/iam/secretsmanager/ecr/s3/route53/rds/dms/elbv2/kms/
// docdb/elasticache/eks/sesv2/acm/wafv2/cloudwatchlogs/ssm/organizations/...),
// *.sql.crossplane.io (mysql/postgresql), *.m.crossplane.io (gitlab
// projects/groups), *.signoz.crossplane.io (alert/notificationchannel), and
// external-secrets.io (+ generators.external-secrets.io).
//
// TODO(config): expose this as an xpc.yaml knob (cluster-capture-groups) so
// multi-cloud coverage is configurable without a code change, mirroring the
// Part 0/D config-knob pattern.
var providerGroupSuffixes = []string{
	".aws.upbound.io",
	"aws.upbound.io", // the bare provider-family group (e.g. aws.upbound.io ProviderConfig)
	".sql.crossplane.io",
	".m.crossplane.io",
	".signoz.crossplane.io",
	"signoz.crossplane.io",
	"external-secrets.io",
	"generators.external-secrets.io",
}

// fixedTargets are the always-attempted, non-discovered resource selectors.
// Each entry is the kubectl resource argument (a CRD plural or short name)
// plus whether it is namespaced (drives the -A flag) and a human label used
// in the captured-kind log line.
//
// All but customresourcedefinitions are marked optional: a cluster may run
// without Crossplane apiextensions, without the pkg.crossplane.io package
// manager, or without Argo's ApplicationSet CRD, and a missing resource TYPE
// must not abort the whole capture (it is logged and skipped instead).
var fixedTargets = []target{
	{selector: "customresourcedefinitions", namespaced: false, label: "CRD"},
	{selector: "compositeresourcedefinitions", namespaced: false, label: "XRD", optional: true},
	{selector: "compositions", namespaced: false, label: "Composition", optional: true},
	{selector: "providers.pkg.crossplane.io", namespaced: false, label: "Crossplane Provider", optional: true},
	{selector: "functions.pkg.crossplane.io", namespaced: false, label: "Crossplane Function", optional: true},
	{selector: "configurations.pkg.crossplane.io", namespaced: false, label: "Crossplane Configuration", optional: true},
	{selector: "applications.argoproj.io", namespaced: true, label: "Argo Application", optional: true},
	{selector: "applicationsets.argoproj.io", namespaced: true, label: "Argo ApplicationSet", optional: true},
	{selector: "appprojects.argoproj.io", namespaced: true, label: "Argo AppProject", optional: true},
}

type target struct {
	selector   string
	namespaced bool
	label      string
	// optional marks a target whose resource TYPE may not be installed. When
	// kubectl reports the type is unknown, the target is skipped with a stderr
	// warning rather than aborting the capture. Discovered managed CRDs are
	// never optional (they exist by construction).
	optional bool
}

// Capturer runs kubectl to assemble a Snapshot from a live cluster. The run
// hook is injected so unit tests can exercise argument construction and YAML
// parsing without a cluster; production code leaves it nil and gets the real
// exec-backed runner.
type Capturer struct {
	// KubectlBin is the path to kubectl. Empty means "look up `kubectl` on
	// PATH at first use".
	KubectlBin string
	// Context is the kube-context to pass via --context. Empty means "use the
	// kubeconfig current-context".
	Context string
	// Timeout overrides CaptureTimeout per kubectl call when non-zero.
	Timeout time.Duration

	// run executes kubectl with the given args and returns stdout. Injected
	// for tests; nil selects execRun. The resolved binary path is passed in
	// so the probe's LookPath result is reused.
	run func(ctx context.Context, bin string, args ...string) ([]byte, error)

	resolved string
	probed   bool
}

// probe resolves the kubectl binary. Idempotent. Returns ErrKubectlAbsent when
// the binary cannot be found. Cluster reachability is exercised lazily by the
// first `kubectl get` (and by serverVersion), not here.
func (c *Capturer) probe() error {
	if c.probed {
		return nil
	}
	c.probed = true
	bin := c.KubectlBin
	if bin == "" {
		bin = "kubectl"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrKubectlAbsent, err)
	}
	c.resolved = resolved
	return nil
}

// runner returns the effective command runner (injected hook or real exec).
func (c *Capturer) runner() func(ctx context.Context, bin string, args ...string) ([]byte, error) {
	if c.run != nil {
		return c.run
	}
	return execRun
}

// timeout returns the per-call timeout.
func (c *Capturer) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return CaptureTimeout
}

// baseArgs returns the leading kubectl args common to every call: the
// --context flag (when set) followed by the caller's verb-specific args.
func (c *Capturer) baseArgs(args ...string) []string {
	out := make([]string, 0, len(args)+1)
	if c.Context != "" {
		out = append(out, "--context="+c.Context)
	}
	return append(out, args...)
}

// get runs `kubectl get <selector> [-A] -o yaml` and returns stdout. kubectl
// emits a single `kind: List` document wrapping the objects (or an empty
// items list when the kind is installed but has no instances); unwrapping
// happens in the caller via expandLists.
func (c *Capturer) get(t target) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()
	args := []string{"get", t.selector}
	if t.namespaced {
		args = append(args, "-A")
	}
	args = append(args, "-o", "yaml")
	return c.runner()(ctx, c.resolved, c.baseArgs(args...)...)
}

// serverVersion runs `kubectl version -o json` and extracts
// serverVersion.gitVersion. A failure here is non-fatal (returns "") because
// an unreachable version endpoint should not sink an otherwise successful
// capture.
func (c *Capturer) serverVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	out, err := c.runner()(ctx, c.resolved, c.baseArgs("version", "-o", "json")...)
	if err != nil {
		return ""
	}
	return parseServerVersion(out)
}

// discoverManagedCRDs lists CRDs and returns the resource selectors
// ("<plural>.<storageVersion>.<group>") whose group matches a provider suffix.
// Selecting by fully-qualified name avoids short-name collisions across
// providers.
//
// The STORAGE version is pinned deliberately: a bare "<plural>.<group>" get
// requests the preferred SERVED version, and when that differs from the
// storage version (upjet providers serve v1beta2 while objects are stored as
// v1beta1) the API server pushes every stored object through the CRD's
// conversion webhook. On large collections that times out server-side — on
// the facilitygrid-ops cluster, listing 341 taskdefinitions.ecs.aws.upbound.io
// at v1beta2 exceeded the API server's 60s ceiling every time, while the same
// list at the stored v1beta1 returned in ~2.3s — which sank the whole capture
// (and with it the hourly drift audit). Listing at the storage version is a
// straight etcd read, no conversion.
func (c *Capturer) discoverManagedCRDs() ([]target, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()
	// jsonpath keeps the payload tiny vs full -o yaml: one line per CRD of
	// "<plural> <group> <scope> <storageVersion>".
	const jp = `{range .items[*]}{.spec.names.plural}{" "}{.spec.group}{" "}{.spec.scope}{" "}{.spec.versions[?(@.storage==true)].name}{"\n"}{end}`
	out, err := c.runner()(ctx, c.resolved,
		c.baseArgs("get", "customresourcedefinitions", "-o", "jsonpath="+jp)...)
	if err != nil {
		return nil, fmt.Errorf("discover CRDs: %w", err)
	}
	return parseManagedCRDs(string(out)), nil
}

// Capture builds a Snapshot from the live cluster. It probes kubectl (hard
// error if absent), discovers managed-resource CRDs, captures every fixed and
// discovered kind, unwraps the kubectl List documents, builds a World, and
// produces a snapshot with IncludeResources:true. The captured kind list is
// logged to stderr so coverage is explicit (no silent GVK truncation).
func (c *Capturer) Capture(clusterName string) (*snapshot.Snapshot, error) {
	if err := c.probe(); err != nil {
		return nil, err
	}

	k8sVersion := c.serverVersion()

	managed, err := c.discoverManagedCRDs()
	if err != nil {
		return nil, err
	}

	targets := make([]target, 0, len(fixedTargets)+len(managed))
	targets = append(targets, fixedTargets...)
	targets = append(targets, managed...)

	var allDocs []loader.LoadedDocument
	captured := make([]string, 0, len(targets))
	for _, t := range targets {
		out, err := c.get(t)
		if err != nil {
			if t.optional && isMissingResourceType(err) {
				fmt.Fprintf(os.Stderr, "clustersrc: skipping %s (resource type not installed)\n", t.selector)
				continue
			}
			return nil, fmt.Errorf("kubectl get %s: %w", t.selector, err)
		}
		docs, err := loader.LoadReader(bytes.NewReader(out), "kubectl://"+t.selector)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", t.selector, err)
		}
		// kubectl wraps a collection in a single kind:List document; the
		// loader does not unwrap it, so expand each List into its items.
		docs, err = expandLists(docs, "kubectl://"+t.selector)
		if err != nil {
			return nil, fmt.Errorf("unwrap %s: %w", t.selector, err)
		}
		allDocs = append(allDocs, docs...)
		captured = append(captured, fmt.Sprintf("%s (%d)", t.selector, len(docs)))
	}

	sort.Strings(captured)
	fmt.Fprintf(os.Stderr, "clustersrc: captured %d kinds: %s\n",
		len(captured), strings.Join(captured, ", "))

	builder := ir.NewBuilder()
	// Cluster objects are already-rendered concrete manifests; there is
	// nothing for Helm/Kustomize/Crossplane to render, and the binaries may
	// not be present where a capture runs. Skip render to stay self-contained.
	builder.SkipRender = true
	// Do NOT expand ApplicationSets: a live cluster already contains the
	// Applications Argo materialized from each AppSet, so expansion would
	// double-count them (real + synthetic) and corrupt every downstream plan
	// diff. The materialized Applications are the source of truth here.
	builder.SkipAppSetExpand = true
	world, err := builder.Build(allDocs)
	if err != nil {
		return nil, fmt.Errorf("build IR from cluster: %w", err)
	}

	snap := snapshot.FromWorldWithOptions(world, clusterName,
		snapshot.FromWorldOptions{IncludeResources: true})
	// The IR builder never sets KubernetesVersion (it is a Snapshot-only
	// field); stamp it and recompute the content digest so it is covered.
	snap.KubernetesVersion = k8sVersion
	snap.ComputeDigest()
	return snap, nil
}

// expandLists unwraps kubectl `kind: List` documents into their constituent
// items. `kubectl get <kind> -o yaml` for a collection returns ONE document
// of kind "List" with an items[] array, not a stream of manifests, and the
// loader has no List-unwrapping. Each item is re-marshalled and re-parsed
// through LoadReader so it picks up the same APIVersion/Kind classification a
// committed manifest would. Non-List documents pass through unchanged.
func expandLists(docs []loader.LoadedDocument, sourcePath string) ([]loader.LoadedDocument, error) {
	var out []loader.LoadedDocument
	for _, d := range docs {
		items, ok := listItems(d)
		if !ok {
			out = append(out, d)
			continue
		}
		for _, item := range items {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			raw, err := yaml.Marshal(m)
			if err != nil {
				return nil, fmt.Errorf("re-marshal list item: %w", err)
			}
			expanded, err := loader.LoadReader(bytes.NewReader(raw), sourcePath)
			if err != nil {
				return nil, fmt.Errorf("re-parse list item: %w", err)
			}
			out = append(out, expanded...)
		}
	}
	return out, nil
}

// listItems returns the items[] of a doc when it is a Kubernetes List
// (kind ends with "List" and items is a slice). Returns (nil,false) otherwise.
func listItems(d loader.LoadedDocument) ([]interface{}, bool) {
	if !strings.HasSuffix(d.Kind, "List") || d.Raw == nil {
		return nil, false
	}
	items, ok := d.Raw["items"].([]interface{})
	if !ok {
		return nil, false
	}
	return items, true
}

// execRun is the production command runner.
func execRun(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("kubectl %s: timed out", strings.Join(args, " "))
		}
		return nil, fmt.Errorf("kubectl %s: %v: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// isMissingResourceType reports whether a kubectl error means the requested
// resource TYPE is not installed in the cluster (as opposed to a real
// failure). These are the messages kubectl prints for an unknown kind.
func isMissingResourceType(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "doesn't have a resource type") ||
		strings.Contains(msg, "the server could not find the requested resource") ||
		strings.Contains(msg, "could not find the requested resource")
}

// parseServerVersion extracts serverVersion.gitVersion from
// `kubectl version -o json` output. Returns "" when absent or unparseable.
func parseServerVersion(out []byte) string {
	var v struct {
		ServerVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"serverVersion"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return ""
	}
	return v.ServerVersion.GitVersion
}

// parseManagedCRDs turns the jsonpath "<plural> <group> <scope>
// <storageVersion>" lines into capture targets, keeping only groups that match
// a provider suffix. When the storage version is present the selector pins it
// ("<plural>.<version>.<group>") so the get is conversion-free (see
// discoverManagedCRDs); a missing version falls back to the bare form.
func parseManagedCRDs(s string) []target {
	var out []target
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		plural, group := fields[0], fields[1]
		if !matchesProviderGroup(group) {
			continue
		}
		namespaced := true
		if len(fields) >= 3 {
			namespaced = !strings.EqualFold(fields[2], "Cluster")
		}
		selector := plural + "." + group
		if len(fields) >= 4 && fields[3] != "" {
			selector = plural + "." + fields[3] + "." + group
		}
		out = append(out, target{
			selector:   selector,
			namespaced: namespaced,
			label:      selector,
		})
	}
	return out
}

// matchesProviderGroup reports whether a CRD group is a managed-resource
// group we want to capture.
func matchesProviderGroup(group string) bool {
	for _, suffix := range providerGroupSuffixes {
		if group == suffix || strings.HasSuffix(group, suffix) {
			return true
		}
	}
	return false
}

// IsKubectlAbsent reports whether err is an ErrKubectlAbsent wrapper.
func IsKubectlAbsent(err error) bool { return errors.Is(err, ErrKubectlAbsent) }
