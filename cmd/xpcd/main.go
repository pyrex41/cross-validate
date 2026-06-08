// Command xpcd is the runtime companion to the static xpc analyzer. It runs
// the same Shen kernel as a Kubernetes ValidatingWebhook, restricted to the
// "decidable subset" of rules that are sound to evaluate on a single live
// object (see docs/adr/005-runtime-decidable-subset.md). Every decision is
// emitted as a structured observability event and reflected in Prometheus
// metrics, so the cluster can report on Argo CD / Crossplane actions in real
// time. The default mode is audit (log-only, never blocks).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pyrex41/cross-validate-/pkg/clustersrc"
	"github.com/pyrex41/cross-validate-/pkg/runtime/controller"
	"github.com/pyrex41/cross-validate-/pkg/runtime/obs"
	"github.com/pyrex41/cross-validate-/pkg/runtime/policy"
)

// version is overridable at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		os.Exit(runServe(os.Args[2:]))
	case "watch":
		os.Exit(runWatch(os.Args[2:]))
	case "version", "--version", "-v":
		fmt.Println("xpcd", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "xpcd: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `xpcd — runtime policy daemon for Argo CD + Crossplane

Usage:
  xpcd serve [flags]   Start the admission webhook + metrics servers
  xpcd watch [flags]   Run the periodic cluster-sweep controller (observe-only)
  xpcd version         Print version

Run "xpcd serve --help" or "xpcd watch --help" for flags.
`)
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	mode := fs.String("mode", obs.ModeAudit, "enforcement mode: audit (log-only) or enforce (deny)")
	addr := fs.String("addr", ":8443", "address for the TLS admission webhook server")
	metricsAddr := fs.String("metrics-addr", ":9090", "address for the metrics + health server")
	certDir := fs.String("cert-dir", "/etc/xpcd/tls", "directory holding tls.crt and tls.key")
	kernelPath := fs.String("kernel-path", "", "path to the Shen kernel directory (default: embedded)")
	clickhouseURL := fs.String("clickhouse-url", os.Getenv("XPCD_CLICKHOUSE_URL"), "optional event sink endpoint (ClickHouse HTTP / collector)")
	_ = fs.Parse(args)

	if *mode != obs.ModeAudit && *mode != obs.ModeEnforce {
		fmt.Fprintf(os.Stderr, "xpcd: invalid --mode %q (want audit or enforce)\n", *mode)
		return 2
	}

	// Observability sinks: always log decisions as JSONL on stdout; add the
	// async HTTP sink when a collector URL is configured.
	metrics := obs.NewMetrics()
	sinks := []obs.Sink{obs.NewStdoutSink(os.Stdout)}
	var httpSink *obs.HTTPSink
	if *clickhouseURL != "" {
		httpSink = obs.NewHTTPSink(*clickhouseURL)
		sinks = append(sinks, httpSink)
	}
	sink := obs.NewMultiSink(sinks...)
	defer sink.Close()

	subset := policy.DecidableSubset()
	srv := &server{
		eval:    policy.New(*kernelPath, subset),
		sink:    sink,
		metrics: metrics,
		mode:    *mode,
	}

	// Readiness flips true once the Shen kernel is warm. Warming happens off
	// the request path so the first real admission isn't penalised.
	var ready atomic.Bool
	srv.ready = ready.Load
	go func() {
		warmKernel(srv.eval)
		ready.Store(true)
	}()

	fmt.Fprintf(os.Stderr, "xpcd %s starting: mode=%s webhook=%s metrics=%s subset=%d rules\n",
		version, *mode, *addr, *metricsAddr, len(subset))
	if httpSink != nil {
		fmt.Fprintf(os.Stderr, "xpcd: event sink -> %s\n", *clickhouseURL)
	}

	// Metrics + health server (plain HTTP).
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsMux.HandleFunc("/healthz", srv.handleHealthz)
	metricsMux.HandleFunc("/readyz", srv.handleReadyz)
	metricsServer := &http.Server{Addr: *metricsAddr, Handler: metricsMux, ReadHeaderTimeout: 5 * time.Second}

	// Admission webhook server (TLS).
	webhookMux := http.NewServeMux()
	webhookMux.HandleFunc("/admit", srv.handleValidate)
	webhookServer := &http.Server{Addr: *addr, Handler: webhookMux, ReadHeaderTimeout: 5 * time.Second}

	errCh := make(chan error, 2)
	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("metrics server: %w", err)
		}
	}()
	go func() {
		crt := filepath.Join(*certDir, "tls.crt")
		key := filepath.Join(*certDir, "tls.key")
		if fileExists(crt) && fileExists(key) {
			if err := webhookServer.ListenAndServeTLS(crt, key); err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("webhook server (tls): %w", err)
			}
			return
		}
		// No certs on disk: fall back to plain HTTP. Useful for local runs;
		// in-cluster the API server requires TLS, so this path warns loudly.
		fmt.Fprintf(os.Stderr, "xpcd: WARNING no tls.crt/tls.key in %s, serving webhook over plain HTTP\n", *certDir)
		if err := webhookServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("webhook server: %w", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		fmt.Fprintf(os.Stderr, "xpcd: %v\n", err)
		return 1
	case sig := <-stop:
		fmt.Fprintf(os.Stderr, "xpcd: received %s, shutting down\n", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = webhookServer.Shutdown(ctx)
	_ = metricsServer.Shutdown(ctx)
	return 0
}

func runWatch(args []string) int {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	interval := fs.Duration("interval", 60*time.Second, "reconcile sweep interval")
	cluster := fs.String("cluster", "default", "cluster name label applied to emitted events")
	kubeContext := fs.String("kube-context", "", "kubectl --context to use (empty: current-context)")
	metricsAddr := fs.String("metrics-addr", ":9090", "address for the metrics + health server")
	kernelPath := fs.String("kernel-path", "", "path to the Shen kernel directory (default: embedded)")
	clickhouseURL := fs.String("clickhouse-url", os.Getenv("XPCD_CLICKHOUSE_URL"), "optional event sink endpoint (ClickHouse HTTP / collector)")
	once := fs.Bool("once", false, "run a single sweep and exit (for CronJob / debugging)")
	mode := fs.String("mode", obs.ModeAudit, "mode label applied to events (the controller is always observe-only)")
	_ = fs.Parse(args)

	metrics := obs.NewMetrics()
	sinks := []obs.Sink{obs.NewStdoutSink(os.Stdout)}
	if *clickhouseURL != "" {
		sinks = append(sinks, obs.NewHTTPSink(*clickhouseURL))
	}
	sink := obs.NewMultiSink(sinks...)
	defer sink.Close()

	reconciler := &controller.Reconciler{
		Capturer:    &clustersrc.Capturer{Context: *kubeContext},
		ClusterName: *cluster,
		KernelPath:  *kernelPath,
		Mode:        *mode,
		Sink:        sink,
		Metrics:     metrics,
	}

	// --once: a single synchronous sweep, no servers (CronJob / debug path).
	if *once {
		s, err := reconciler.ReconcileOnce(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "xpcd watch: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "xpcd watch: swept %d resources, %d with violations\n", s.Resources, s.Violations)
		return 0
	}

	fmt.Fprintf(os.Stderr, "xpcd %s watching cluster=%s every %s, metrics=%s, subset=%d rules\n",
		version, *cluster, *interval, *metricsAddr, len(reconciler.Subset))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Metrics + health server. Readiness flips after the first sweep completes.
	var ready atomic.Bool
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ready.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	ms := &http.Server{Addr: *metricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := ms.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "xpcd watch: metrics server: %v\n", err)
		}
	}()

	sweep := func() {
		s, err := reconciler.ReconcileOnce(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "xpcd watch: sweep failed: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "xpcd watch: swept %d resources, %d with violations (%v)\n",
				s.Resources, s.Violations, s.Duration.Round(time.Millisecond))
		}
		ready.Store(true)
	}

	sweep() // immediate first sweep
	t := time.NewTicker(*interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "xpcd watch: shutting down")
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = ms.Shutdown(sctx)
			cancel()
			return 0
		case <-t.C:
			sweep()
		}
	}
}

// warmKernel triggers the one-time Shen runtime initialisation by evaluating a
// trivial object, so the first real admission request is fast. Errors are
// non-fatal — the daemon fails open.
func warmKernel(e *policy.Evaluator) {
	const probe = `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"xpcd-warmup"}}`
	_, _ = e.Evaluate([]byte(probe), policy.ObjectRef{Version: "v1", Kind: "ConfigMap", Name: "xpcd-warmup"}, nil)
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
