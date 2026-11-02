// Command mcp-server runs the service-directory MCP server: streamable HTTP on
// 127.0.0.1:$PORT/mcp, hub JWT verification, and GET /healthz for readiness.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Arif-Karim/mcp-hub/examples/mcp-server-template/internal/app"
	"github.com/Arif-Karim/mcp-hub/examples/mcp-server-template/internal/config"
	"github.com/Arif-Karim/mcp-hub/examples/mcp-server-template/internal/hubauth"
)

// shutdownGrace bounds how long in-flight requests get after SIGTERM.
const shutdownGrace = 10 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("exiting", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	v := &hubauth.Verifier{
		Keys:     hubauth.NewJWKS(cfg.JWKSURL, nil),
		Issuer:   cfg.Issuer,
		Audience: cfg.Audience,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.Addr)
	if err != nil {
		return err
	}
	logger.Info("listening", "addr", cfg.Addr, "audience", cfg.Audience)
	return serve(ctx, ln, app.Handler(v, logger), logger)
}

// serve runs h on ln until ctx is cancelled, then shuts down gracefully.
//
// Every request context derives from baseCtx, which is cancelled as soon as
// Shutdown starts. Long-lived requests (the MCP standalone SSE stream that
// clients such as Claude Code keep open) therefore end immediately instead of
// holding Shutdown until its deadline.
func serve(ctx context.Context, ln net.Listener, h http.Handler, logger *slog.Logger) error {
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	srv := &http.Server{
		Handler:           h,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No ReadTimeout/WriteTimeout: they would cut long-lived SSE streams.
	}
	srv.RegisterOnShutdown(cancelBase)

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
