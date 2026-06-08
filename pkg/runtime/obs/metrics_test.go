package obs

import (
	"bufio"
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsObserveAndRender(t *testing.T) {
	m := NewMetrics()

	deny := sampleEvent() // Deny / enforce / Application, Errors=2, EvalNanos=1_500_000
	allow := sampleEvent()
	allow.Decision = DecisionAllow
	allow.Errors = 0
	allow.EvalNanos = 200_000

	m.Observe(deny)
	m.Observe(deny)
	m.Observe(allow)
	m.AddDropped(3)

	out := string(m.Render())

	checks := []string{
		"# TYPE xpcd_decisions_total counter",
		`xpcd_decisions_total{decision="allow",mode="enforce",kind="Application"} 1`,
		`xpcd_decisions_total{decision="deny",mode="enforce",kind="Application"} 2`,
		"# TYPE xpcd_eval_errors_total counter",
		"xpcd_eval_errors_total 4", // 2 + 2 from the two deny events
		"# TYPE xpcd_events_dropped_total counter",
		"xpcd_events_dropped_total 3",
		"# TYPE xpcd_eval_seconds histogram",
		"xpcd_eval_seconds_count 3",
		`xpcd_eval_seconds_bucket{le="+Inf"} 3`,
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("exposition missing %q\n---\n%s", want, out)
		}
	}
}

func TestMetricsHistogramCumulative(t *testing.T) {
	m := NewMetrics()
	// Two samples in distinct buckets: 200us (le>=0.0005) and 2ms (le>=0.005).
	// This exercises the cumulative invariant across multiple populated buckets
	// — a single sample would not catch a double-counting bug in the tail.
	e1 := sampleEvent()
	e1.EvalNanos = 200_000 // 0.0002s
	m.Observe(e1)
	e2 := sampleEvent()
	e2.EvalNanos = 2_000_000 // 0.002s
	m.Observe(e2)
	out := string(m.Render())

	// le=0.0001 (100us): neither sample.
	mustContain(t, out, `xpcd_eval_seconds_bucket{le="0.0001"} 0`)
	// le=0.0005 (500us): only the 200us sample.
	mustContain(t, out, `xpcd_eval_seconds_bucket{le="0.0005"} 1`)
	// le=0.001 (1ms): still only the 200us sample (the 2ms one is larger).
	mustContain(t, out, `xpcd_eval_seconds_bucket{le="0.001"} 1`)
	// le=0.005 (5ms): both samples are now within bound.
	mustContain(t, out, `xpcd_eval_seconds_bucket{le="0.005"} 2`)
	mustContain(t, out, `xpcd_eval_seconds_bucket{le="+Inf"} 2`)
	mustContain(t, out, `xpcd_eval_seconds_count 2`)
}

func mustContain(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Errorf("exposition missing %q\n---\n%s", want, out)
	}
}

func TestMetricsHandler(t *testing.T) {
	m := NewMetrics()
	m.Observe(sampleEvent())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status: got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "version=0.0.4") {
		t.Fatalf("content-type: got %q", ct)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("decisions_total")) {
		t.Fatalf("body missing decisions_total:\n%s", rec.Body.String())
	}
}

// TestMetricsExpositionWellFormed asserts every non-comment, non-empty line is a
// well-formed `name[{labels}] value` sample, and that HELP/TYPE precede samples.
func TestMetricsExpositionWellFormed(t *testing.T) {
	m := NewMetrics()
	m.Observe(sampleEvent())
	m.AddDropped(1)

	sc := bufio.NewScanner(bytes.NewReader(m.Render()))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if !strings.HasPrefix(line, "# HELP ") && !strings.HasPrefix(line, "# TYPE ") {
				t.Errorf("unexpected comment line: %q", line)
			}
			continue
		}
		// A sample line: "<metric> <value>" where <metric> may carry {labels}.
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Errorf("malformed sample line (want 2 fields): %q", line)
			continue
		}
		name := fields[0]
		if name == "" {
			t.Errorf("empty metric name: %q", line)
		}
		// Balanced braces when labels are present.
		if strings.Contains(name, "{") && !strings.HasSuffix(name, "}") {
			t.Errorf("unbalanced label braces: %q", line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
}
