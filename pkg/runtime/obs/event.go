// Package obs turns runtime policy decisions into observable signals.
//
// The daemon evaluates Argo CD / Crossplane actions at runtime (admission
// webhooks, controllers) and every decision is funneled through this package,
// which emits:
//
//   - structured JSONL events suitable for ClickHouse ingestion (see Sink),
//   - Prometheus-format metrics (see Metrics).
//
// Only the Go standard library is used.
package obs

import (
	"encoding/json"
	"time"
)

// Decision values.
const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
	DecisionWarn  = "warn"
	// DecisionWouldDeny marks an audit-mode object that carried an
	// error-severity diagnostic: it was admitted, but enforce mode would
	// have blocked it. It lets operators measure enforce impact pre-rollout.
	DecisionWouldDeny = "would-deny"
)

// Mode values.
const (
	ModeEnforce = "enforce"
	ModeAudit   = "audit"
)

// Operation values.
const (
	OperationCreate = "CREATE"
	OperationUpdate = "UPDATE"
	OperationDelete = "DELETE"
)

// Event is a single runtime policy decision rendered as an observable signal.
//
// The JSON encoding is deliberately flat and ClickHouse-friendly (one row per
// event, JSONEachRow style). Timestamp marshals as RFC3339Nano.
type Event struct {
	Timestamp time.Time `json:"ts"`
	Decision  string    `json:"decision"` // allow / deny / warn
	Mode      string    `json:"mode"`     // enforce / audit

	Group     string `json:"group"`
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`

	Operation string `json:"operation"` // CREATE / UPDATE / DELETE

	RuleCodes []string `json:"ruleCodes"` // codes of rules that fired
	Errors    int      `json:"errors"`
	Warnings  int      `json:"warnings"`

	EvalNanos int64  `json:"evalNanos"`
	Message   string `json:"message"`
	Source    string `json:"source"` // admission / controller / ...
}

// MarshalLine renders the event as a single newline-terminated JSON line
// (JSONL / JSONEachRow). The trailing newline makes the result directly
// appendable to a JSONL stream or a ClickHouse HTTP insert body.
func (e Event) MarshalLine() ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
