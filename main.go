package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	listen := flag.String("listen", envOr("LISTEN", ":4319"), "listen address")
	upstream := flag.String("upstream", envOr("OTLP_UPSTREAM", "http://127.0.0.1:4318"), "OTLP HTTP upstream base URL")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	u, err := url.Parse(*upstream)
	if err != nil || u.Scheme == "" || u.Host == "" {
		log.Error("invalid upstream", "upstream", *upstream, "error", err)
		os.Exit(1)
	}

	bridge := NewBridge(u, log)
	server := &http.Server{
		Addr:              *listen,
		Handler:           bridge.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Error("listen failed", "addr", *listen, "error", err)
		os.Exit(1)
	}
	log.Info("otlp-ws-bridge listening", "addr", ln.Addr().String(), "upstream", u.String())
	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
