package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/runtime/obs"
	"github.com/pyrex41/cross-validate-/pkg/runtime/policy"
)

// stateBearingNoOrphan is a state-bearing Crossplane managed resource (Aurora
// Cluster) with no deletionPolicy — the INC-6 / R23 failure shape. It is a
// single self-contained object, so the decidable subset can judge it at
// admission time without any cluster context.
const stateBearingNoOrphan = `{
  "apiVersion": "rds.aws.upbound.io/v1beta1",
  "kind": "Cluster",
  "metadata": {"name": "aurora-prod-cluster", "namespace": "crossplane-system"},
  "spec": {"forProvider": {"region": "us-east-1", "engine": "aurora-postgresql", "databaseName": "app"}}
}`

const stateBearingOrphan = `{
  "apiVersion": "rds.aws.upbound.io/v1beta1",
  "kind": "Cluster",
  "metadata": {"name": "aurora-prod-cluster", "namespace": "crossplane-system"},
  "spec": {"deletionPolicy": "Orphan", "forProvider": {"region": "us-east-1", "engine": "aurora-postgresql", "databaseName": "app"}}
}`

const r23Code = "XPC.S.crossplane-state-needs-orphan"

func newTestServer(t *testing.T, mode string) (*server, *recordingSink) {
	t.Helper()
	rec := &recordingSink{}
	return &server{
		eval:    policy.New("", policy.DecidableSubset()),
		sink:    rec,
		metrics: obs.NewMetrics(),
		mode:    mode,
	}, rec
}

type recordingSink struct{ events []obs.Event }

func (r *recordingSink) Emit(e obs.Event) { r.events = append(r.events, e) }
func (r *recordingSink) Close() error     { return nil }

func reviewJSON(t *testing.T, object string) []byte {
	t.Helper()
	review := AdmissionReview{
		APIVersion: "admission.k8s.io/v1",
		Kind:       "AdmissionReview",
		Request: &AdmissionRequest{
			UID:       "test-uid-1",
			Kind:      GroupVersionKind{Group: "rds.aws.upbound.io", Version: "v1beta1", Kind: "Cluster"},
			Name:      "aurora-prod-cluster",
			Namespace: "crossplane-system",
			Operation: "CREATE",
			Object:    json.RawMessage(object),
		},
	}
	b, err := json.Marshal(review)
	if err != nil {
		t.Fatalf("marshal review: %v", err)
	}
	return b
}

func post(t *testing.T, srv *server, body []byte) AdmissionReview {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admit", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleValidate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var out AdmissionReview
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	if out.Response == nil {
		t.Fatal("response is nil")
	}
	if out.Response.UID != "test-uid-1" {
		t.Errorf("UID = %q, want echoed test-uid-1", out.Response.UID)
	}
	return out
}

func TestAuditModeAllowsButWarns(t *testing.T) {
	srv, rec := newTestServer(t, obs.ModeAudit)
	out := post(t, srv, reviewJSON(t, stateBearingNoOrphan))

	if !out.Response.Allowed {
		t.Fatal("audit mode must allow the request")
	}
	if !containsCode(out.Response.Warnings, r23Code) {
		t.Errorf("expected R23 in warnings, got %v", out.Response.Warnings)
	}
	if len(rec.events) != 1 {
		t.Fatalf("want 1 event, got %d", len(rec.events))
	}
	if got := rec.events[0].Decision; got != obs.DecisionWouldDeny {
		t.Errorf("decision = %q, want would-deny", got)
	}
	if rec.events[0].Errors == 0 {
		t.Error("expected error count > 0 on the event")
	}
}

func TestEnforceModeDenies(t *testing.T) {
	srv, rec := newTestServer(t, obs.ModeEnforce)
	out := post(t, srv, reviewJSON(t, stateBearingNoOrphan))

	if out.Response.Allowed {
		t.Fatal("enforce mode must deny the R23 violation")
	}
	if out.Response.Status == nil || !strings.Contains(out.Response.Status.Message, r23Code) {
		t.Errorf("deny status should name R23, got %+v", out.Response.Status)
	}
	if got := rec.events[0].Decision; got != obs.DecisionDeny {
		t.Errorf("decision = %q, want deny", got)
	}
}

func TestCompliantObjectAllowed(t *testing.T) {
	srv, rec := newTestServer(t, obs.ModeEnforce)
	out := post(t, srv, reviewJSON(t, stateBearingOrphan))

	if !out.Response.Allowed {
		t.Fatalf("orphaned resource must be allowed; warnings=%v", out.Response.Warnings)
	}
	if got := rec.events[0].Decision; got != obs.DecisionAllow {
		t.Errorf("decision = %q, want allow", got)
	}
}

func TestMalformedReviewFailsOpen(t *testing.T) {
	srv, _ := newTestServer(t, obs.ModeEnforce)
	req := httptest.NewRequest(http.MethodPost, "/admit", bytes.NewReader([]byte("{not json")))
	w := httptest.NewRecorder()
	srv.handleValidate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fail open)", w.Code)
	}
	var out AdmissionReview
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Response == nil || !out.Response.Allowed {
		t.Error("malformed review must fail open (allowed)")
	}
}

func containsCode(warnings []string, code string) bool {
	for _, w := range warnings {
		if strings.Contains(w, code) {
			return true
		}
	}
	return false
}
