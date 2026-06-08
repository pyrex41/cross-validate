package obs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStdoutSinkWritesJSONL(t *testing.T) {
	var buf bytes.Buffer
	s := NewStdoutSink(&buf)
	defer s.Close()

	e1 := sampleEvent()
	e2 := sampleEvent()
	e2.Decision = DecisionAllow
	s.Emit(e1)
	s.Emit(e2)

	sc := bufio.NewScanner(&buf)
	var n int
	for sc.Scan() {
		var got Event
		if err := json.Unmarshal(sc.Bytes(), &got); err != nil {
			t.Fatalf("line %d not valid JSON: %v (%q)", n, err, sc.Text())
		}
		n++
	}
	if n != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d", n)
	}
}

func TestStdoutSinkDefaultWriter(t *testing.T) {
	s := NewStdoutSink(nil)
	if s.w == nil {
		t.Fatal("expected default writer to be set")
	}
}

func TestHTTPSinkDeliversEvents(t *testing.T) {
	var (
		mu       sync.Mutex
		received []Event
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/x-ndjson" {
			t.Errorf("unexpected content type %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		sc := bufio.NewScanner(bytes.NewReader(body))
		mu.Lock()
		for sc.Scan() {
			if len(bytes.TrimSpace(sc.Bytes())) == 0 {
				continue
			}
			var e Event
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				t.Errorf("server got invalid JSON: %v", err)
				continue
			}
			received = append(received, e)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	const total = 25
	s := NewHTTPSink(srv.URL,
		WithBatchSize(10),
		WithFlushInterval(50*time.Millisecond),
		WithBufferSize(1024),
	)
	for i := 0; i < total; i++ {
		s.Emit(sampleEvent())
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	mu.Lock()
	got := len(received)
	mu.Unlock()
	if got != total {
		t.Fatalf("expected %d events delivered, got %d (sent=%d dropped=%d)",
			total, got, s.Sent(), s.Dropped())
	}
	if s.Sent() != total {
		t.Fatalf("expected Sent()=%d, got %d", total, s.Sent())
	}
}

func TestHTTPSinkRetriesOnceThenCountsDrop(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := NewHTTPSink(srv.URL, WithBatchSize(1), WithFlushInterval(time.Hour))
	s.Emit(sampleEvent())
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Fatalf("expected 2 POST attempts (initial + retry), got %d", c)
	}
	if s.Dropped() != 1 {
		t.Fatalf("expected 1 dropped event after failure, got %d", s.Dropped())
	}
}

func TestHTTPSinkDropsWhenBufferFull(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // hold the worker so the buffer can fill
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(block)

	// Tiny buffer, large batch so the worker pulls one event and blocks on POST.
	s := NewHTTPSink(srv.URL,
		WithBufferSize(1),
		WithBatchSize(1),
		WithFlushInterval(time.Millisecond),
		WithTimeout(50*time.Millisecond),
	)

	// Fire many events quickly; with the worker blocked, the buffer overflows.
	for i := 0; i < 1000; i++ {
		s.Emit(sampleEvent())
	}

	if s.Dropped() == 0 {
		t.Fatalf("expected some dropped events with a tiny buffer, got 0")
	}
}

func TestHTTPSinkCloseIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := NewHTTPSink(srv.URL)
	if err := s.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestMultiSinkFanout(t *testing.T) {
	var a, b bytes.Buffer
	m := NewMultiSink(NewStdoutSink(&a), NewStdoutSink(&b), NopSink{})
	m.Emit(sampleEvent())
	if err := m.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if a.Len() == 0 || b.Len() == 0 {
		t.Fatalf("expected both sinks to receive output, got a=%d b=%d", a.Len(), b.Len())
	}
}

func TestNopSink(t *testing.T) {
	var n NopSink
	n.Emit(sampleEvent())
	if err := n.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
