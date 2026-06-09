package clustersrc

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/loader"
)

func TestProbeAbsentKubectl(t *testing.T) {
	c := &Capturer{KubectlBin: "/nonexistent/xpc-test-kubectl"}
	_, err := c.Capture("test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsKubectlAbsent(err) {
		t.Fatalf("expected ErrKubectlAbsent wrapper, got %v", err)
	}
}

func TestParseServerVersion(t *testing.T) {
	js := `{"serverVersion":{"gitVersion":"v1.29.4-eks-036c24b"}}`
	if got := parseServerVersion([]byte(js)); got != "v1.29.4-eks-036c24b" {
		t.Fatalf("parseServerVersion = %q", got)
	}
	if got := parseServerVersion([]byte("not json")); got != "" {
		t.Fatalf("parseServerVersion(garbage) = %q, want empty", got)
	}
}

func TestParseManagedCRDsFiltersByGroup(t *testing.T) {
	in := strings.Join([]string{
		"instances ec2.aws.upbound.io Cluster v1beta1",
		"externalsecrets external-secrets.io Namespaced v1beta1",
		"projects projects.gitlab.m.crossplane.io Cluster v1alpha1",
		"widgets example.com Namespaced v1", // not a provider group -> dropped
		"legacies ec2.aws.upbound.io Cluster", // no storage version reported -> bare selector
		"",
	}, "\n")
	got := parseManagedCRDs(in)
	if len(got) != 4 {
		t.Fatalf("got %d targets, want 4: %+v", len(got), got)
	}
	bySel := map[string]target{}
	for _, tg := range got {
		bySel[tg.selector] = tg
	}
	// The selector must pin the STORAGE version: an unversioned get requests
	// the preferred served version and pays per-object conversion-webhook cost
	// (which times out on large collections, e.g. ECS taskdefinitions).
	if tg, ok := bySel["instances.v1beta1.ec2.aws.upbound.io"]; !ok || tg.namespaced {
		t.Fatalf("ec2 instances: want cluster-scoped version-pinned target, got %+v (ok=%v)", tg, ok)
	}
	if tg, ok := bySel["externalsecrets.v1beta1.external-secrets.io"]; !ok || !tg.namespaced {
		t.Fatalf("externalsecrets: want namespaced version-pinned target, got %+v (ok=%v)", tg, ok)
	}
	if _, ok := bySel["legacies.ec2.aws.upbound.io"]; !ok {
		t.Fatalf("missing storage version must fall back to the bare selector: %+v", got)
	}
}

// TestExpandLists is the regression guard for the kubectl `kind: List` wrapper:
// `kubectl get <kind> -o yaml` returns ONE List document, not a manifest
// stream, and the loader does not unwrap it. Without expandLists every
// captured resource is silently dropped.
func TestExpandLists(t *testing.T) {
	listYAML := []byte(`apiVersion: v1
kind: List
items:
- apiVersion: ec2.aws.upbound.io/v1beta1
  kind: Instance
  metadata:
    name: web-1
- apiVersion: ec2.aws.upbound.io/v1beta1
  kind: Instance
  metadata:
    name: web-2
`)
	docs, err := loader.LoadReader(strings.NewReader(string(listYAML)), "kubectl://instances")
	if err != nil {
		t.Fatalf("LoadReader: %v", err)
	}
	// Sanity: the loader sees the wrapper as a single List doc, not 2 Instances.
	if len(docs) != 1 || docs[0].Kind != "List" {
		t.Fatalf("pre-unwrap: got %d docs (first kind %q), want 1 List", len(docs), docs[0].Kind)
	}
	expanded, err := expandLists(docs, "kubectl://instances")
	if err != nil {
		t.Fatalf("expandLists: %v", err)
	}
	if len(expanded) != 2 {
		t.Fatalf("post-unwrap: got %d docs, want 2", len(expanded))
	}
	for _, d := range expanded {
		if d.Kind != "Instance" {
			t.Fatalf("unwrapped doc kind = %q, want Instance", d.Kind)
		}
	}

	// An empty installed kind returns kind:List with items:[] -> 0 docs, no leak.
	emptyDocs, _ := loader.LoadReader(strings.NewReader("apiVersion: v1\nkind: List\nitems: []\n"), "kubectl://empty")
	gotEmpty, err := expandLists(emptyDocs, "kubectl://empty")
	if err != nil {
		t.Fatalf("expandLists(empty): %v", err)
	}
	if len(gotEmpty) != 0 {
		t.Fatalf("empty List expanded to %d docs, want 0", len(gotEmpty))
	}
}

// listOf wraps object YAML bodies in the kubectl `kind: List` envelope that
// `kubectl get -o yaml` actually emits.
func listOf(items ...string) []byte {
	if len(items) == 0 {
		return []byte("apiVersion: v1\nkind: List\nitems: []\n")
	}
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: List\nitems:\n")
	for _, it := range items {
		for i, line := range strings.Split(strings.TrimRight(it, "\n"), "\n") {
			if i == 0 {
				b.WriteString("- ")
			} else {
				b.WriteString("  ")
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

func TestCaptureArgConstructionAndParsing(t *testing.T) {
	var calls [][]string
	run := func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		calls = append(calls, args)
		joined := strings.Join(args, " ")
		switch {
		// jsonpath before version: the discovery jsonpath itself contains
		// ".spec.versions" and must not match the version probe.
		case strings.Contains(joined, "jsonpath"):
			return []byte("instances ec2.aws.upbound.io Cluster v1beta1\n"), nil
		case strings.Contains(joined, "version"):
			return []byte(`{"serverVersion":{"gitVersion":"v1.30.0"}}`), nil
		case strings.Contains(joined, "instances.v1beta1.ec2.aws.upbound.io"):
			return listOf(`apiVersion: ec2.aws.upbound.io/v1beta1
kind: Instance
metadata:
  name: web-1`), nil
		case strings.Contains(joined, "applications.argoproj.io"):
			return listOf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app-1
  namespace: argocd
spec:
  destination: {}
  source: {}`), nil
		default:
			return listOf(), nil // empty (but installed) List for other fixed kinds
		}
	}

	c := &Capturer{Context: "prod-eks", run: run}
	c.probed = true
	c.resolved = "kubectl"

	snap, err := c.Capture("prod")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if snap.KubernetesVersion != "v1.30.0" {
		t.Fatalf("KubernetesVersion = %q", snap.KubernetesVersion)
	}
	// These prove the List unwrap actually reached the IR — the whole point.
	if len(snap.Resources) == 0 {
		t.Fatal("expected captured Resources (List unwrap failed?), got none")
	}
	if len(snap.ArgoApps) == 0 {
		t.Fatal("expected captured ArgoApps (List unwrap failed?), got none")
	}

	sawContext := false
	for _, args := range calls {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "--context=prod-eks") {
			sawContext = true
		}
		if strings.Contains(joined, "instances.v1beta1.ec2.aws.upbound.io") && strings.Contains(joined, " -A") {
			t.Fatalf("cluster-scoped MR get must not pass -A: %v", args)
		}
		if strings.Contains(joined, "applications.argoproj.io") && !strings.Contains(joined, "-A") {
			t.Fatalf("namespaced Argo get must pass -A: %v", args)
		}
	}
	if !sawContext {
		t.Fatal("--context=prod-eks was never propagated to kubectl")
	}
}

// TestCaptureSkipsMissingOptionalType verifies a cluster missing an optional
// resource type (e.g. no ApplicationSet CRD) does NOT abort the capture.
func TestCaptureSkipsMissingOptionalType(t *testing.T) {
	run := func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "jsonpath"):
			return []byte(""), nil // no managed CRDs
		case strings.Contains(joined, "version"):
			return []byte(`{"serverVersion":{"gitVersion":"v1.30.0"}}`), nil
		case strings.Contains(joined, "applicationsets.argoproj.io"):
			return nil, errors.New(`error: the server doesn't have a resource type "applicationsets"`)
		default:
			return listOf(), nil
		}
	}
	c := &Capturer{run: run}
	c.probed = true
	c.resolved = "kubectl"

	if _, err := c.Capture("prod"); err != nil {
		t.Fatalf("Capture aborted on a missing optional type: %v", err)
	}
}

func TestIsMissingResourceType(t *testing.T) {
	yes := fmt.Errorf(`kubectl get applicationsets: exit 1: error: the server doesn't have a resource type "applicationsets"`)
	if !isMissingResourceType(yes) {
		t.Fatal("expected missing-resource-type to be recognized")
	}
	no := errors.New("kubectl get instances: connection refused")
	if isMissingResourceType(no) {
		t.Fatal("connection error misclassified as missing resource type")
	}
}

// TestCaptureRealCluster is the e2e gate. It runs only when kubectl is on PATH
// and a cluster is reachable; otherwise it skips so hermetic CI stays green.
func TestCaptureRealCluster(t *testing.T) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not on PATH; skipping live capture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "kubectl", "version", "-o", "json").Run(); err != nil {
		t.Skip("no reachable cluster; skipping live capture")
	}
	c := &Capturer{}
	snap, err := c.Capture("e2e")
	if err != nil {
		t.Fatalf("live Capture: %v", err)
	}
	if snap.Digest == "" {
		t.Fatal("live snapshot has empty digest")
	}
}
