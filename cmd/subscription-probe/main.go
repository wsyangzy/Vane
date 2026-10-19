// subscription-probe is an opt-in, isolated T0 harness. It never reads a
// client's token/config files or registers itself with the desktop app.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"ai-relay-forwarder/internal/providers/claude/proxy/subscriptionprobe"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18081", "isolated loopback listener")
	names := flag.String("callers", "t0-client", "comma-separated independent local caller labels")
	maxRequests := flag.Uint64("max-requests", 16, "maximum actual official sends for this isolated run")
	emitEvidence := flag.Bool("evidence", false, "write bounded nonsecret request evidence as JSON lines to stderr")
	flag.Parse()
	if *maxRequests == 0 {
		fmt.Fprintln(os.Stderr, "max-requests must be positive")
		os.Exit(1)
	}
	registry := subscriptionprobe.NewRegistry()
	listener, err := subscriptionprobe.ListenLocal(*listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// This explicit stdout disclosure is the one-time local Relay key delivery,
	// not an OAuth token or an application log/event. No request data is logged.
	fmt.Printf("T0 probe listening on %s\n", listener.Addr())
	for _, name := range strings.Split(*names, ",") {
		if strings.TrimSpace(name) == "" {
			continue
		}
		caller, key, err := registry.Create(strings.TrimSpace(name))
		if err != nil {
			fmt.Fprintln(os.Stderr, "caller initialization failed")
			_ = listener.Close()
			os.Exit(1)
		}
		fmt.Printf("Caller: %s (%s)\nX-Relay-Key (shown once): %s\n", caller.Name, caller.ID, key)
	}
	options := []subscriptionprobe.Option{subscriptionprobe.WithAttemptLimit(*maxRequests)}
	if *emitEvidence {
		options = append(options, subscriptionprobe.WithEvidence(func(evidence subscriptionprobe.Evidence) {
			// No raw errors, headers, caller names, credentials or bodies.
			_ = json.NewEncoder(os.Stderr).Encode(evidence)
		}))
	}
	server := &http.Server{Handler: subscriptionprobe.New(registry, nil, nil, "", options...), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Minute}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "probe server stopped")
		os.Exit(1)
	}
}
