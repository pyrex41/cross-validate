package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pyrex41/cross-validate-/pkg/ir"
	"github.com/pyrex41/cross-validate-/pkg/loader"
	"github.com/pyrex41/cross-validate-/pkg/runtime/obs"
	"github.com/pyrex41/cross-validate-/pkg/snapshot"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// twoClusters has one state-bearing managed resource missing deletionPolicy
// (R23 violation) and one with Orphan set (clean). The controller should emit
// exactly one would-deny event, for the first.
const twoClusters = `
apiVersion: rds.aws.upbound.io/v1beta1
kind: Cluster
metadata:
  name: aurora-prod-bad
  namespace: crossplane-system
spec:
  forProvider:
    region: us-east-1
    engine: aurora-postgresql
    databaseName: app
---
apiVersion: rds.aws.upbound.io/v1beta1
kind: Cluster
metadata:
  name: aurora-prod-good
  namespace: crossplane-system
spec:
  deletionPolicy: Orphan
  forProvider:
    region: us-east-1
    engine: aurora-postgresql
    databaseName: app
`

// fakeCapturer returns a pre-built snapshot, standing in for a live cluster.
type fakeCapturer struct {
	snap *snapshot.Snapshot
	err  error
}

func (f *fakeCapturer) Capture(string) (*snapshot.Snapshot, error) {
	return f.snap, f.err
}

func snapshotFromYAML(t *testing.T, yaml string) *snapshot.Snapshot {
	t.Helper()
	docs, err := loader.LoadReader(strings.NewReader(yaml), "test://world")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	world, err := ir.NewBuilder().Build(docs)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return snapshot.FromWorldWithOptions(world, "test", snapshot.FromWorldOptions{IncludeResources: true})
}

func TestReconcileEmitsViolationEvents(t *testing.T) {
	rec := &recordingSink{}
	metrics := obs.NewMetrics()
	r := &Reconciler{
		Capturer:    &fakeCapturer{snap: snapshotFromYAML(t, twoClusters)},
		ClusterName: "test",
		Sink:        rec,
		Metrics:     metrics,
		Now:         func() time.Time { return time.Unix(1717800000, 0) },
	}

	summary, err := r.ReconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if summary.Resources != 2 {
		t.Errorf("Resources = %d, want 2", summary.Resources)
	}
	if summary.Violations != 1 {
		t.Fatalf("Violations = %d, want 1 (only the no-orphan cluster)", summary.Violations)
	}
	if len(rec.events) != 1 {
		t.Fatalf("emitted %d events, want 1", len(rec.events))
	}

	ev := rec.events[0]
	if ev.Source != "controller" {
		t.Errorf("source = %q, want controller", ev.Source)
	}
	if ev.Decision != obs.DecisionWouldDeny {
		t.Errorf("decision = %q, want would-deny", ev.Decision)
	}
	if ev.Name != "aurora-prod-bad" {
		t.Errorf("name = %q, want aurora-prod-bad (attributed to the offending object)", ev.Name)
	}
	if ev.Kind != "Cluster" || ev.Group != "rds.aws.upbound.io" {
		t.Errorf("gvk = %s/%s, want rds.aws.upbound.io/Cluster", ev.Group, ev.Kind)
	}
	if !containsCode(ev.RuleCodes, "XPC.S.crossplane-state-needs-orphan") {
		t.Errorf("ruleCodes = %v, want R23 code", ev.RuleCodes)
	}
	if ev.Timestamp.IsZero() {
		t.Error("event timestamp not stamped")
	}

	// Metrics reflect the sweep.
	out := string(metrics.Render())
	mustContain(t, out, "xpcd_controller_runs_total 1")
	mustContain(t, out, "xpcd_controller_resources_scanned 2")
	mustContain(t, out, "xpcd_controller_violations 1")
}

// divergeYAML is a live managed resource whose spec.forProvider.taskDefinition
// (a registered canonical field) diverges from its observed status.atProvider —
// the reconcile-storm fingerprint R32 catches. R32 needs observed status, which
// only the controller sweep has, so this exercises the TierLive unlock.
const divergeYAML = `
apiVersion: ecs.aws.upbound.io/v1beta1
kind: Service
metadata: {name: app-svc, namespace: crossplane-system}
spec:
  deletionPolicy: Orphan
  forProvider:
    taskDefinition: "arn:aws:ecs:us-east-1:1:task-definition/app:42"
status:
  atProvider:
    taskDefinition: "arn:aws:ecs:us-east-1:1:task-definition/app:43"
`

func TestReconcileUnlocksR32OnLiveStatus(t *testing.T) {
	rec := &recordingSink{}
	r := &Reconciler{
		Capturer:    &fakeCapturer{snap: snapshotFromYAML(t, divergeYAML)},
		ClusterName: "prod",
		Sink:        rec,
		Metrics:     obs.NewMetrics(),
	}
	s, err := r.ReconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if s.Violations != 1 || len(rec.events) != 1 {
		t.Fatalf("want 1 R32 violation, got violations=%d events=%d", s.Violations, len(rec.events))
	}
	ev := rec.events[0]
	if !containsCode(ev.RuleCodes, "XPC.M.observed-desired-fixed-point") {
		t.Errorf("ruleCodes = %v, want R32", ev.RuleCodes)
	}
	if ev.Cluster != "prod" {
		t.Errorf("cluster label = %q, want prod", ev.Cluster)
	}
	if ev.Source != "controller" {
		t.Errorf("source = %q, want controller", ev.Source)
	}
}

func TestReconcileCaptureErrorPropagates(t *testing.T) {
	r := &Reconciler{
		Capturer:    &fakeCapturer{err: errFake},
		ClusterName: "test",
		Sink:        &recordingSink{},
		Metrics:     obs.NewMetrics(),
	}
	if _, err := r.ReconcileOnce(context.Background()); err == nil {
		t.Fatal("expected capture error to propagate")
	}
}

func TestReconcileCleanClusterEmitsNothing(t *testing.T) {
	clean := `
apiVersion: rds.aws.upbound.io/v1beta1
kind: Cluster
metadata: {name: ok, namespace: crossplane-system}
spec:
  deletionPolicy: Orphan
  forProvider: {region: us-east-1, engine: aurora-postgresql, databaseName: app}
`
	rec := &recordingSink{}
	r := &Reconciler{
		Capturer:    &fakeCapturer{snap: snapshotFromYAML(t, clean)},
		ClusterName: "test",
		Sink:        rec,
		Metrics:     obs.NewMetrics(),
	}
	s, err := r.ReconcileOnce(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if s.Violations != 0 || len(rec.events) != 0 {
		t.Errorf("clean cluster: violations=%d events=%d, want 0/0", s.Violations, len(rec.events))
	}
}

// --- test helpers ---

type recordingSink struct{ events []obs.Event }

func (r *recordingSink) Emit(e obs.Event) { r.events = append(r.events, e) }
func (r *recordingSink) Close() error     { return nil }

func containsCode(codes []string, want string) bool {
	for _, c := range codes {
		if c == want {
			return true
		}
	}
	return false
}

func mustContain(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Errorf("metrics missing %q\n%s", want, out)
	}
}

var errFake = fakeErr("kubectl unreachable")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

// ensure types import is used even if helpers change
var _ = types.SeverityError
