// Command mcp-server runs the service-directory MCP server: streamable HTTP on
// 127.0.0.1:$PORT/mcp, hub JWT verification, and GET /healthz for readiness.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Arif-Karim/mcp-hub/examples/mcp-server-template/internal/app"
	"github.com/Arif-Karim/mcp-hub/examples/mcp-server-template/internal/config"
	"github.com/Arif-Karim/mcp-hub/examples/mcp-server-template/internal/hubauth"
)

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
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler(v, logger),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr, "audience", cfg.Audience)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
