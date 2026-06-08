package obs

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// evalBuckets are the fixed upper bounds (in seconds) for the eval latency
// histogram. The implicit +Inf bucket is added at exposition time.
var evalBuckets = []float64{
	0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5,
}

// decisionLabels is the label tuple keying the decisions_total counter.
type decisionLabels struct {
	decision string
	mode     string
	kind     string
}

// Metrics is a tiny hand-rolled Prometheus registry for runtime policy signals.
//
// It exposes:
//   - xpcd_decisions_total{decision,mode,kind}  (counter)
//   - xpcd_eval_errors_total                     (counter)
//   - xpcd_events_dropped_total                  (counter)
//   - xpcd_eval_seconds{sum,count,bucket}        (histogram)
//
// All updates and scrapes are guarded by a single mutex.
type Metrics struct {
	mu sync.Mutex

	decisions  map[decisionLabels]uint64
	evalErrors uint64
	dropped    uint64

	evalCount   uint64
	evalSum     float64   // seconds
	evalBuckets []uint64  // cumulative-ready per-bucket counts (le ordering)
	bounds      []float64 // copy of evalBuckets bounds
}

// NewMetrics returns an empty, ready-to-use Metrics registry.
func NewMetrics() *Metrics {
	return &Metrics{
		decisions:   make(map[decisionLabels]uint64),
		evalBuckets: make([]uint64, len(evalBuckets)),
		bounds:      evalBuckets,
	}
}

// Observe updates all metrics from a single Event.
func (m *Metrics) Observe(e Event) {
	seconds := float64(e.EvalNanos) / 1e9

	m.mu.Lock()
	defer m.mu.Unlock()

	m.decisions[decisionLabels{decision: e.Decision, mode: e.Mode, kind: e.Kind}]++
	m.evalErrors += uint64(e.Errors)

	m.evalCount++
	m.evalSum += seconds
	for i, b := range m.bounds {
		if seconds <= b {
			m.evalBuckets[i]++
		}
	}
}

// AddDropped records n dropped events (e.g. reported by an HTTPSink).
func (m *Metrics) AddDropped(n uint64) {
	m.mu.Lock()
	m.dropped += n
	m.mu.Unlock()
}

// Handler returns an http.Handler serving the metrics in Prometheus text
// exposition format (version 0.0.4).
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write(m.Render())
	})
}

// Render produces the Prometheus exposition payload.
func (m *Metrics) Render() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()

	var b strings.Builder

	// xpcd_decisions_total
	b.WriteString("# HELP xpcd_decisions_total Total runtime policy decisions by outcome, mode and kind.\n")
	b.WriteString("# TYPE xpcd_decisions_total counter\n")
	keys := make([]decisionLabels, 0, len(m.decisions))
	for k := range m.decisions {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].decision != keys[j].decision {
			return keys[i].decision < keys[j].decision
		}
		if keys[i].mode != keys[j].mode {
			return keys[i].mode < keys[j].mode
		}
		return keys[i].kind < keys[j].kind
	})
	for _, k := range keys {
		fmt.Fprintf(&b, "xpcd_decisions_total{decision=%s,mode=%s,kind=%s} %d\n",
			quote(k.decision), quote(k.mode), quote(k.kind), m.decisions[k])
	}

	// xpcd_eval_errors_total
	b.WriteString("# HELP xpcd_eval_errors_total Total policy evaluation errors observed.\n")
	b.WriteString("# TYPE xpcd_eval_errors_total counter\n")
	fmt.Fprintf(&b, "xpcd_eval_errors_total %d\n", m.evalErrors)

	// xpcd_events_dropped_total
	b.WriteString("# HELP xpcd_events_dropped_total Total observability events dropped before delivery.\n")
	b.WriteString("# TYPE xpcd_events_dropped_total counter\n")
	fmt.Fprintf(&b, "xpcd_events_dropped_total %d\n", m.dropped)

	// xpcd_eval_seconds histogram. m.evalBuckets[i] already holds the count of
	// samples with value <= bounds[i] (Observe increments every satisfied
	// bound), so each is emitted directly — no running sum.
	b.WriteString("# HELP xpcd_eval_seconds Policy evaluation latency in seconds.\n")
	b.WriteString("# TYPE xpcd_eval_seconds histogram\n")
	for i, bound := range m.bounds {
		fmt.Fprintf(&b, "xpcd_eval_seconds_bucket{le=%s} %d\n", quote(formatFloat(bound)), m.evalBuckets[i])
	}
	fmt.Fprintf(&b, "xpcd_eval_seconds_bucket{le=\"+Inf\"} %d\n", m.evalCount)
	fmt.Fprintf(&b, "xpcd_eval_seconds_sum %s\n", formatFloat(m.evalSum))
	fmt.Fprintf(&b, "xpcd_eval_seconds_count %d\n", m.evalCount)

	return []byte(b.String())
}

// quote renders a Prometheus label value with the required escaping.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
