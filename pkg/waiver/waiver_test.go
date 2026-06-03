package waiver

import (
	"testing"
	"time"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

func diag(code, file, msg string) types.Diagnostic {
	return types.Diagnostic{
		Code:     code,
		Severity: types.SeverityError,
		Message:  msg,
		Source:   types.SourceLocation{File: file, Line: 1},
	}
}

var jun3 = time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)

func TestApply_SuppressesMatch(t *testing.T) {
	s := &Set{Source: "/repo/.xpc-waivers.yaml", Waivers: []Waiver{{
		Rule: "XPC.S.crossplane-state-needs-orphan",
		File: "deploy/foo/docdb-prod-cluster.yaml",
		Kind: "Cluster", Name: "docdb-prod-cluster",
		Reason: "tracked in ENG-1234", ExpiresAt: "2026-12-01",
	}}}
	diags := []types.Diagnostic{
		diag("XPC.S.crossplane-state-needs-orphan",
			"/abs/deploy/foo/docdb-prod-cluster.yaml",
			"Cluster/docdb-prod-cluster is a state-bearing resource without deletionPolicy: Orphan"),
		diag("XPC.S.crossplane-state-needs-orphan",
			"/abs/deploy/bar/other.yaml",
			"Cluster/other is a state-bearing resource without deletionPolicy: Orphan"),
	}
	res := s.Apply(diags, jun3)
	if len(res.Waived) != 1 {
		t.Fatalf("expected 1 waived, got %d", len(res.Waived))
	}
	// The other finding (different file/name) survives; no unused/expired noise.
	if len(res.Active) != 1 {
		t.Fatalf("expected 1 active, got %d: %+v", len(res.Active), res.Active)
	}
	if res.Active[0].Message != diags[1].Message {
		t.Fatalf("wrong finding survived: %q", res.Active[0].Message)
	}
}

func TestApply_ExpiredRefiresAndWarns(t *testing.T) {
	s := &Set{Source: "/repo/.xpc-waivers.yaml", Waivers: []Waiver{{
		Rule: "XPC012", Name: "e2e-pool-replenish", Kind: "CronJob",
		Reason: "lazy bootstrap configmap", ExpiresAt: "2026-05-01", // past
	}}}
	diags := []types.Diagnostic{
		diag("XPC012", "/abs/deploy/x/cronjob.yaml",
			"ConfigMap pool-seed is absent ... mounted by CronJob/e2e-pool-replenish"),
	}
	res := s.Apply(diags, jun3)
	if len(res.Waived) != 0 {
		t.Fatalf("expired waiver must not suppress; got %d waived", len(res.Waived))
	}
	if res.ExpiredCount != 1 {
		t.Fatalf("expected ExpiredCount 1, got %d", res.ExpiredCount)
	}
	// Active = original re-fired finding + the synthetic expired warning.
	var sawOriginal, sawWarning bool
	for _, d := range res.Active {
		if d.Code == "XPC012" {
			sawOriginal = true
		}
		if d.Code == CodeWaiverExpired && d.Severity == types.SeverityWarning {
			sawWarning = true
		}
	}
	if !sawOriginal || !sawWarning {
		t.Fatalf("expected re-fired finding + expired warning; active=%+v", res.Active)
	}
}

func TestApply_UnusedWaiverInfo(t *testing.T) {
	s := &Set{Source: "/repo/.xpc-waivers.yaml", Waivers: []Waiver{{
		Rule: "XPC.B.providerconfig-resolves", Name: "nonexistent",
		Reason: "x", ExpiresAt: "2026-12-01",
	}}}
	res := s.Apply(nil, jun3)
	if res.UnusedCount != 1 {
		t.Fatalf("expected UnusedCount 1, got %d", res.UnusedCount)
	}
	if len(res.Active) != 1 || res.Active[0].Code != CodeWaiverUnused {
		t.Fatalf("expected one waiver-unused info, got %+v", res.Active)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		w    Waiver
		ok   bool
	}{
		{"good", Waiver{Rule: "R", Name: "n", Reason: "why", ExpiresAt: "2026-12-01"}, true},
		{"no-rule", Waiver{Name: "n", Reason: "why", ExpiresAt: "2026-12-01"}, false},
		{"no-target", Waiver{Rule: "R", Reason: "why", ExpiresAt: "2026-12-01"}, false},
		{"no-reason", Waiver{Rule: "R", Name: "n", ExpiresAt: "2026-12-01"}, false},
		{"no-expiry", Waiver{Rule: "R", Name: "n", Reason: "why"}, false},
		{"bad-expiry", Waiver{Rule: "R", Name: "n", Reason: "why", ExpiresAt: "next tuesday"}, false},
	}
	for _, c := range cases {
		err := (&Set{Waivers: []Waiver{c.w}}).Validate()
		if c.ok && err != nil {
			t.Errorf("%s: expected valid, got %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: expected invalid, got nil", c.name)
		}
	}
}

func TestFileMatches(t *testing.T) {
	if !fileMatches("/abs/repo/deploy/x/c.yaml", "deploy/x/c.yaml") {
		t.Error("suffix path should match")
	}
	if !fileMatches("/abs/repo/deploy/x/c.yaml", "c.yaml") {
		t.Error("bare basename should match")
	}
	if fileMatches("/abs/repo/deploy/x/c.yaml", "deploy/y/c.yaml") {
		t.Error("different dir must not match")
	}
	if !fileMatches("/anything", "") {
		t.Error("empty constraint matches anything")
	}
}
