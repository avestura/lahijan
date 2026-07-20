// Package main implements the WS-22 e2e harness.
//
// The harness is a tiny Go program (build tag `e2e`) that hosts the
// in-process Incus + PowerDNS fakes on fixed ports. The Lahijan app
// (built separately from cmd/lahijan) connects to these URLs at startup
// via the standard LAHIJAN_PROVIDERS_* env vars; from the app's point of
// view these look like real daemons. The SeaweedFS backend stays real
// (the WS-22 test compose ships a `weed mini` container) because the
// storage fake is operations-interface-level, not wire-level, so it
// cannot serve the data-plane S3 traffic the dashboard + aws-cli drive.
//
// Why a separate binary and not a *testing.T-driven test?
//   - program.Start() calls log.Fatalf on bootstrap errors (os.Exit),
//     so it cannot be driven from a TestMain. Running the app as a
//     subprocess keeps the lifecycle simple + lets the harness script
//     trap+kill on exit.
//   - The fakes are long-lived (the app's lifetime, not a single test's);
//     a standalone binary is the natural shape.
//
// Build & run:
//
//	# Terminal 1 — host the fakes
//	go run -tags e2e ./test/e2e/harness
//
//	# Terminal 2 — start the Lahijan app pointed at the fakes
//	LAHIJAN_PROVIDERS_INCUS_REMOTEURL=http://localhost:18091 \
//	LAHIJAN_PROVIDERS_POWERDNS_BASEURL=http://localhost:18092 \
//	./dist/lahijan
//
// Or use the runner script: scripts/run-e2e.sh does both + Playwright.
//
//go:build e2e

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	incusfake "github.com/avestura/lahijan/internal/app/lahijan/providers/incus/fake"
	powerdnsfake "github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns/fake"
)

const (
	defaultIncusPort    = 18091
	defaultPowerdnsPort = 18092
)

func main() {
	incusPort := flag.Int("incus-port", defaultIncusPort, "Port to listen on for the fake Incus REST API")
	powerdnsPort := flag.Int("powerdns-port", defaultPowerdnsPort, "Port to listen on for the fake PowerDNS HTTP API")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// The httptest fakes pick a random port themselves; expose them on
	// the well-known ports the runner script agrees on by reverse-proxying.
	incusFake := incusfake.NewServerStandalone()
	powerdnsFake := powerdnsfake.NewServerStandalone()
	defer incusFake.HTTP.Close()
	defer powerdnsFake.HTTP.Close()

	logger.Info("fakes started",
		"incus_internal", incusFake.HTTP.URL,
		"powerdns_internal", powerdnsFake.HTTP.URL,
	)

	incusProxy := newReverseProxy(incusFake.HTTP.URL, *incusPort, logger.With("fake", "incus"))
	powerdnsProxy := newReverseProxy(powerdnsFake.HTTP.URL, *powerdnsPort, logger.With("fake", "powerdns"))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go incusProxy.run(ctx)
	go powerdnsProxy.run(ctx)

	logger.Info("harness ready — blocking until SIGINT/SIGTERM")
	<-ctx.Done()
	logger.Info("harness shutting down")
	// Give the proxies a moment to drain; their serve loops check ctx.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	<-shutdownCtx.Done()
}

// reverseProxy fronts the given backend URL on a fixed listen port. The
// fakes bind random ports themselves; we proxy so the Lahijan app + the
// runner script can agree on a stable URL across runs.
type reverseProxy struct {
	backend string
	port    int
	logger  *slog.Logger
	srv     *http.Server
}

func newReverseProxy(backendURL string, port int, logger *slog.Logger) *reverseProxy {
	return &reverseProxy{backend: backendURL, port: port, logger: logger}
}

func (p *reverseProxy) run(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handler)

	p.srv = &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", p.port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		p.logger.Info("listening", "port", p.port, "backend", p.backend)
		if err := p.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.logger.Error("listen error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.srv.Shutdown(shutdownCtx); err != nil {
		p.logger.Warn("shutdown error", "error", err)
	}
}

func (p *reverseProxy) handler(w http.ResponseWriter, r *http.Request) {
	// Build a forwarding request. We can't use httputil.NewSingleHostReverseProxy
	// directly because the fakes are httptest servers (already fully formed);
	// this manual forward keeps the body + headers + websocket upgrade path
	// intact (the Incus events + exec endpoints rely on the upgrade).
	out, err := http.NewRequestWithContext(r.Context(), r.Method, p.backend+r.URL.Path, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// Copy headers (incl. websocket upgrade keys).
	for k, vs := range r.Header {
		for _, v := range vs {
			out.Header.Add(k, v)
		}
	}
	out.URL.RawQuery = r.URL.RawQuery

	resp, err := http.DefaultTransport.RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	// Stream the body. Tolerate nil/NoBody (rare 204/304) + silently
	// ignore write errors mid-stream (the client may have disconnected;
	// the status code is already written).
	if resp.Body != nil && resp.Body != http.NoBody {
		_, _ = io.Copy(w, resp.Body)
	}
}
