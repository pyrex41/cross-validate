package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/pyrex41/cross-validate-/pkg/runtime/obs"
	"github.com/pyrex41/cross-validate-/pkg/runtime/policy"
	"github.com/pyrex41/cross-validate-/pkg/types"
)

// server holds the wiring shared by the admission and health handlers.
type server struct {
	eval    *policy.Evaluator
	sink    obs.Sink
	metrics *obs.Metrics
	mode    string // obs.ModeAudit or obs.ModeEnforce

	// ready is flipped once the Shen kernel has been warmed, so the
	// readiness probe only reports ready when evaluations can actually run.
	ready func() bool
}

// handleValidate implements the Kubernetes ValidatingWebhook contract. It
// always responds 200 with an AdmissionReview; a deny is expressed in the
// response body, never as an HTTP error (an HTTP error would be governed by the
// webhook's failurePolicy, which we deliberately set to Ignore).
func (s *server) handleValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var review AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil || review.Request == nil {
		// Malformed review: fail open. We cannot echo a UID, so return a
		// bare allow — failurePolicy: Ignore makes this the safe default.
		malformed := obs.Event{Timestamp: time.Now(), Decision: obs.DecisionAllow, Mode: s.mode, Source: "admission"}
		s.sink.Emit(malformed)
		s.metrics.Observe(malformed)
		writeReview(w, reviewResponse(review.APIVersion, review.Kind,
			allowResponse("", []string{"xpcd: malformed AdmissionReview, skipped"})))
		return
	}

	resp, event := s.evaluate(review.Request)
	s.sink.Emit(event)
	s.metrics.Observe(event)
	writeReview(w, reviewResponse(review.APIVersion, review.Kind, resp))
}

// evaluate runs the decidable subset against a single admitted object and maps
// the resulting Decision onto an AdmissionResponse plus an observability Event.
func (s *server) evaluate(req *AdmissionRequest) (*AdmissionResponse, obs.Event) {
	// On DELETE the live object is in OldObject; on CREATE/UPDATE it is in
	// Object. Evaluating the object on DELETE turns the INC-6 floor into a
	// real deletion guard (block removal of state-bearing resources that
	// never set deletionPolicy: Orphan).
	raw := req.Object
	if len(raw) == 0 {
		raw = req.OldObject
	}

	ref := policy.ObjectRef{
		Group:     req.Kind.Group,
		Version:   req.Kind.Version,
		Kind:      req.Kind.Kind,
		Name:      req.Name,
		Namespace: req.Namespace,
		UID:       req.UID,
	}

	base := obs.Event{
		Timestamp: time.Now(),
		Mode:      s.mode,
		Group:     ref.Group,
		Version:   ref.Version,
		Kind:      ref.Kind,
		Name:      ref.Name,
		Namespace: ref.Namespace,
		UID:       ref.UID,
		Operation: req.Operation,
		Source:    "admission",
	}

	if len(raw) == 0 {
		base.Decision = obs.DecisionAllow
		return allowResponse(req.UID, nil), base
	}

	dec, err := s.eval.Evaluate(raw, ref, nil)
	if err != nil {
		// Evaluation failure is an xpcd-internal problem, not a manifest
		// problem: fail open, count it, surface a warning.
		base.Decision = obs.DecisionAllow
		base.Errors = 1
		base.Message = "xpcd evaluation error: " + err.Error()
		return allowResponse(req.UID, []string{base.Message}), base
	}

	codes, warnings := summarize(dec.Diagnostics)
	base.RuleCodes = codes
	base.Errors = dec.Errors
	base.Warnings = dec.Warnings
	base.EvalNanos = dec.EvalNanos

	enforce := s.mode == obs.ModeEnforce

	switch {
	case dec.Errors > 0 && enforce:
		base.Decision = obs.DecisionDeny
		base.Message = fmt.Sprintf("xpcd denied %s/%s: %s", ref.Kind, ref.Name, strings.Join(codes, ", "))
		return denyResponse(req.UID, base.Message+"\n"+strings.Join(warnings, "\n"), warnings), base
	case dec.Errors > 0:
		// Audit mode: never block, but record the would-be denial so the
		// observability layer can report enforce-mode impact ahead of rollout.
		base.Decision = obs.DecisionWouldDeny
		base.Message = fmt.Sprintf("xpcd would deny in enforce mode: %s", strings.Join(codes, ", "))
		return allowResponse(req.UID, append([]string{base.Message}, warnings...)), base
	default:
		// Clean, or warnings-only: allowed. Warnings (if any) ride the
		// AdmissionResponse warnings channel and the event's Warnings count.
		base.Decision = obs.DecisionAllow
		return allowResponse(req.UID, warnings), base
	}
}

// summarize extracts the sorted unique rule codes and a human-readable warning
// line per diagnostic, for the AdmissionResponse warnings channel.
func summarize(diags []types.Diagnostic) (codes []string, warnings []string) {
	seen := map[string]bool{}
	for _, d := range diags {
		if !seen[d.Code] {
			seen[d.Code] = true
			codes = append(codes, d.Code)
		}
		warnings = append(warnings, fmt.Sprintf("%s: %s", d.Code, d.Message))
	}
	sort.Strings(codes)
	return codes, warnings
}

func writeReview(w http.ResponseWriter, review AdmissionReview) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(review)
}

// handleHealthz is a liveness probe: the process is up.
func (s *server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok")
}

// handleReadyz is a readiness probe: the kernel is warm and evaluations work.
func (s *server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	if s.ready == nil || s.ready() {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ready")
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, "warming")
}
