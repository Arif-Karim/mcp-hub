// Command mcp-hub is the local MCP marketplace daemon and CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/claudeconfig"
	"github.com/Arif-Karim/mcp-hub/internal/cli"
	"github.com/Arif-Karim/mcp-hub/internal/daemon"
	"github.com/Arif-Karim/mcp-hub/internal/version"
	"github.com/Arif-Karim/mcp-hub/internal/webui"
)

const defaultAddr = "127.0.0.1:7420"

func main() {
	// Client subcommands (search, info, enable, ...) live in internal/cli and
	// return their own exit codes (2 usage/not approved, 3 daemon down).
	if len(os.Args) > 1 && cli.Handles(os.Args[1]) {
		os.Exit(cli.Run(context.Background(), os.Args[1:], cli.DefaultEnv()))
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-hub:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: mcp-hub <serve|version|search|info|enable|disable|status|help>")
	}
	switch args[0] {
	case "version":
		fmt.Println(version.Version)
		return nil
	case "serve":
		return serve(args[1:])
	default:
		return fmt.Errorf("unknown command %q (run `mcp-hub help`)", args[0])
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", envOr("MCP_HUB_ADDR", defaultAddr), "listen address (loopback only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireLoopback(*addr); err != nil {
		return err
	}

	cat, err := loadCatalogue()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	paths, err := claudeconfig.DefaultPaths()
	if err != nil {
		return err
	}
	source := os.Getenv("MCP_HUB_CATALOGUE")
	if source == "" {
		source = "embedded"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Bind before touching anything: a second daemon fails here ("address in
	// use") instead of restarting duplicate server processes.
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", *addr)
	if err != nil {
		return err
	}
	d, err := daemon.New(daemon.Config{
		Home:             filepath.Dir(paths.Ledger),
		ClaudeConfigPath: paths.ClaudeConfig,
		Addr:             *addr,
		Version:          version.Version,
		Catalogue:        cat,
		CatalogueSource:  source,
		User:             currentUser(),
		UI:               webui.Handler(),
		Logger:           logger,
	})
	if err != nil {
		_ = ln.Close()
		return err
	}
	defer func() { _ = d.Close() }()
	d.Restore(ctx)

	srv := &http.Server{
		Handler:           d.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("mcp-hub listening", "addr", *addr, "version", version.Version, "home", filepath.Dir(paths.Ledger),
			"claudeConfig", paths.ClaudeConfig, "catalogue", source, "enabled", d.Hub.EnabledCount())
		errCh <- srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// requireLoopback refuses to expose the daemon beyond this machine: it hands
// out tokens via the proxy, so a non-loopback bind would leak them to the network.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid addr %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("refusing to listen on non-loopback address %q", addr)
	}
	return nil
}

// loadCatalogue returns the embedded catalogue, or the file named by
// MCP_HUB_CATALOGUE. An invalid catalogue stops startup with every problem listed.
func loadCatalogue() (*catalogue.Store, error) {
	path := os.Getenv("MCP_HUB_CATALOGUE")
	if path == "" {
		return catalogue.Default()
	}
	s, err := catalogue.Load(path)
	if err != nil {
		return nil, fmt.Errorf("catalogue %s:\n%w", path, err)
	}
	return s, nil
}

// currentUser is the subject placed in issued tokens. In production the
// enterprise IdP's identity replaces it.
func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return envOr("USER", "developer")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
