package obs

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Sink consumes Events. Emit must never block the caller's hot path for long;
// implementations that may be slow (network) buffer internally and drop on
// overflow rather than blocking.
type Sink interface {
	Emit(Event)
	Close() error
}

// StdoutSink writes each Event as a JSONL line to an io.Writer.
//
// It is mutex-guarded so concurrent Emit calls produce non-interleaved lines.
type StdoutSink struct {
	mu sync.Mutex
	w  io.Writer
}

// NewStdoutSink returns a StdoutSink writing to w. If w is nil, os.Stdout is
// used.
func NewStdoutSink(w io.Writer) *StdoutSink {
	if w == nil {
		w = os.Stdout
	}
	return &StdoutSink{w: w}
}

// Emit writes the event as a JSONL line. Marshal/write errors are swallowed
// (observability must not crash the policy path).
func (s *StdoutSink) Emit(e Event) {
	line, err := e.MarshalLine()
	if err != nil {
		return
	}
	s.mu.Lock()
	_, _ = s.w.Write(line)
	s.mu.Unlock()
}

// Close is a no-op for StdoutSink (it does not own the writer).
func (s *StdoutSink) Close() error { return nil }

// HTTPSink is an async, buffered POSTer that ships events to a collector
// (ClickHouse HTTP interface or a generic JSONEachRow endpoint).
//
// Events are placed on a buffered channel by Emit (non-blocking: on overflow
// the event is dropped and counted). A background goroutine batches events and
// POSTs newline-delimited JSON, flushing when the batch reaches batchSize or
// when flushInterval elapses, whichever comes first.
type HTTPSink struct {
	url        string
	client     *http.Client
	ch         chan Event
	batchSize  int
	flush      time.Duration
	timeout    time.Duration
	contentTyp string

	dropped uint64 // atomic
	sent    uint64 // atomic

	done   chan struct{}
	closed chan struct{}
	once   sync.Once
}

// HTTPOption configures an HTTPSink.
type HTTPOption func(*HTTPSink)

// WithBatchSize sets the number of events that trigger an immediate flush.
func WithBatchSize(n int) HTTPOption {
	return func(s *HTTPSink) {
		if n > 0 {
			s.batchSize = n
		}
	}
}

// WithFlushInterval sets the maximum time a buffered event waits before flush.
func WithFlushInterval(d time.Duration) HTTPOption {
	return func(s *HTTPSink) {
		if d > 0 {
			s.flush = d
		}
	}
}

// WithBufferSize sets the capacity of the in-memory event channel. Events are
// dropped (and counted) when this buffer is full.
func WithBufferSize(n int) HTTPOption {
	return func(s *HTTPSink) {
		if n > 0 {
			s.ch = make(chan Event, n)
		}
	}
}

// WithTimeout sets the per-POST context timeout.
func WithTimeout(d time.Duration) HTTPOption {
	return func(s *HTTPSink) {
		if d > 0 {
			s.timeout = d
		}
	}
}

// WithHTTPClient overrides the http.Client used for POSTs.
func WithHTTPClient(c *http.Client) HTTPOption {
	return func(s *HTTPSink) {
		if c != nil {
			s.client = c
		}
	}
}

// WithContentType overrides the request Content-Type (default
// "application/x-ndjson").
func WithContentType(ct string) HTTPOption {
	return func(s *HTTPSink) {
		if ct != "" {
			s.contentTyp = ct
		}
	}
}

// NewHTTPSink constructs an HTTPSink targeting url and starts its background
// worker. Call Close to drain and stop it.
func NewHTTPSink(url string, opts ...HTTPOption) *HTTPSink {
	s := &HTTPSink{
		url:        url,
		client:     &http.Client{},
		ch:         make(chan Event, 1024),
		batchSize:  100,
		flush:      time.Second,
		timeout:    5 * time.Second,
		contentTyp: "application/x-ndjson",
		done:       make(chan struct{}),
		closed:     make(chan struct{}),
	}
	for _, o := range opts {
		o(s)
	}
	go s.run()
	return s
}

// Emit enqueues the event without blocking. If the buffer is full the event is
// dropped and the drop counter is incremented, so the policy path is never
// stalled by a slow collector.
func (s *HTTPSink) Emit(e Event) {
	select {
	case s.ch <- e:
	default:
		atomic.AddUint64(&s.dropped, 1)
	}
}

// Dropped returns the number of events dropped due to buffer overflow.
func (s *HTTPSink) Dropped() uint64 { return atomic.LoadUint64(&s.dropped) }

// Sent returns the number of events successfully POSTed.
func (s *HTTPSink) Sent() uint64 { return atomic.LoadUint64(&s.sent) }

// Close signals the worker to drain remaining buffered events and stop. It
// blocks until the worker has finished. Close is idempotent.
func (s *HTTPSink) Close() error {
	s.once.Do(func() { close(s.done) })
	<-s.closed
	return nil
}

func (s *HTTPSink) run() {
	defer close(s.closed)

	ticker := time.NewTicker(s.flush)
	defer ticker.Stop()

	batch := make([]Event, 0, s.batchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		s.post(batch)
		batch = batch[:0]
	}

	for {
		select {
		case e := <-s.ch:
			batch = append(batch, e)
			if len(batch) >= s.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-s.done:
			// Drain everything currently buffered, then flush and exit.
			for {
				select {
				case e := <-s.ch:
					batch = append(batch, e)
					if len(batch) >= s.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

// post encodes batch as newline-delimited JSON and POSTs it, retrying once on
// error. Events in a batch that ultimately fails to send are counted as drops.
func (s *HTTPSink) post(batch []Event) {
	var buf bytes.Buffer
	for _, e := range batch {
		line, err := e.MarshalLine()
		if err != nil {
			continue
		}
		buf.Write(line)
	}
	if buf.Len() == 0 {
		return
	}
	body := buf.Bytes()

	const attempts = 2 // initial + one retry
	for i := 0; i < attempts; i++ {
		if s.doPost(body) {
			atomic.AddUint64(&s.sent, uint64(len(batch)))
			return
		}
	}
	atomic.AddUint64(&s.dropped, uint64(len(batch)))
}

// doPost performs a single POST and reports success. It recovers from panics so
// a misbehaving transport can never take down the worker goroutine.
func (s *HTTPSink) doPost(body []byte) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", s.contentTyp)

	resp, err := s.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// MultiSink fans an Event out to several sinks.
type MultiSink struct {
	sinks []Sink
}

// NewMultiSink returns a MultiSink wrapping the given sinks.
func NewMultiSink(sinks ...Sink) *MultiSink {
	return &MultiSink{sinks: sinks}
}

// Emit forwards the event to every wrapped sink.
func (m *MultiSink) Emit(e Event) {
	for _, s := range m.sinks {
		s.Emit(e)
	}
}

// Close closes every wrapped sink, returning the first error encountered while
// still attempting to close the rest.
func (m *MultiSink) Close() error {
	var first error
	for _, s := range m.sinks {
		if err := s.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// NopSink discards all events.
type NopSink struct{}

// Emit discards the event.
func (NopSink) Emit(Event) {}

// Close is a no-op.
func (NopSink) Close() error { return nil }
