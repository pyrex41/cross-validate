package obs

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func sampleEvent() Event {
	return Event{
		Timestamp: time.Date(2026, 6, 8, 12, 0, 0, 123456789, time.UTC),
		Decision:  DecisionDeny,
		Mode:      ModeEnforce,
		Group:     "argoproj.io",
		Version:   "v1alpha1",
		Kind:      "Application",
		Name:      "guestbook",
		Namespace: "argocd",
		UID:       "abc-123",
		Operation: OperationUpdate,
		RuleCodes: []string{"R001", "R042"},
		Errors:    2,
		Warnings:  1,
		EvalNanos: 1_500_000,
		Message:   "denied by policy",
		Source:    "admission",
	}
}

func TestMarshalLineNewlineTerminated(t *testing.T) {
	e := sampleEvent()
	line, err := e.MarshalLine()
	if err != nil {
		t.Fatalf("MarshalLine: %v", err)
	}
	if len(line) == 0 || line[len(line)-1] != '\n' {
		t.Fatalf("expected newline-terminated line, got %q", line)
	}
	if bytes.Count(line, []byte("\n")) != 1 {
		t.Fatalf("expected exactly one newline, got %d", bytes.Count(line, []byte("\n")))
	}
}

func TestMarshalLineRoundTrip(t *testing.T) {
	e := sampleEvent()
	line, err := e.MarshalLine()
	if err != nil {
		t.Fatalf("MarshalLine: %v", err)
	}

	var got Event
	if err := json.Unmarshal(bytes.TrimSpace(line), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !got.Timestamp.Equal(e.Timestamp) {
		t.Errorf("timestamp: got %v want %v", got.Timestamp, e.Timestamp)
	}
	// Normalize the time for DeepEqual (location pointer may differ).
	got.Timestamp = e.Timestamp
	if !jsonEqual(t, got, e) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, e)
	}
}

func TestMarshalLineRFC3339Nano(t *testing.T) {
	e := sampleEvent()
	line, _ := e.MarshalLine()
	want := `"ts":"2026-06-08T12:00:00.123456789Z"`
	if !bytes.Contains(line, []byte(want)) {
		t.Fatalf("expected RFC3339Nano timestamp %s in %s", want, line)
	}
}

func jsonEqual(t *testing.T, a, b Event) bool {
	t.Helper()
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb)
}
