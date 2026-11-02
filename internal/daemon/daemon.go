// Package daemon assembles the running hub from MCP_HUB_HOME: token issuer,
// audit log, Claude config writer, installer, process runner, the hub
// service and the HTTP handler. cmd/mcp-hub serve and the e2e test share it.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/audit"
	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/claudeconfig"
	"github.com/Arif-Karim/mcp-hub/internal/hub"
	"github.com/Arif-Karim/mcp-hub/internal/proxy"
	"github.com/Arif-Karim/mcp-hub/internal/registry"
	"github.com/Arif-Karim/mcp-hub/internal/runner"
	"github.com/Arif-Karim/mcp-hub/internal/server"
	"github.com/Arif-Karim/mcp-hub/internal/skillfiles"
	"github.com/Arif-Karim/mcp-hub/internal/token"
)

// Issuer is the "iss" claim of hub tokens (and HUB_ISSUER for local servers).
const Issuer = "mcp-hub"

// Config configures New.
type Config struct {
	Home             string // MCP_HUB_HOME
	ClaudeConfigPath string // CLAUDE_CONFIG_PATH
	Addr             string // listen address, e.g. 127.0.0.1:7420 (must be the real port)
	Version          string
	Catalogue        *catalogue.Store
	CatalogueSource  string // "embedded" or the file path
	User             string // token subject + audit user
	UI               http.Handler
	Logger           *slog.Logger
	// RegistryClient downloads artifacts; nil uses the registry default.
	RegistryClient *http.Client
	// ClaudeDir is the Claude Code directory the embedded skill installs into; empty disables it.
	ClaudeDir string
}

// Daemon is a constructed, not yet serving, hub.
type Daemon struct {
	Hub     *hub.Service
	handler http.Handler
	runner  *runner.Runner
	audit   *audit.Log
	logger  *slog.Logger

	closeOnce sync.Once
	closeErr  error
}

// DrainTimeout bounds how long Close waits for in-flight enables/restores.
const DrainTimeout = 15 * time.Second

// New creates MCP_HUB_HOME (0700) and everything under it.
func New(cfg Config) (*Daemon, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Home == "" || cfg.ClaudeConfigPath == "" || cfg.Catalogue == nil {
		return nil, errors.New("daemon: Home, ClaudeConfigPath and Catalogue are required")
	}
	for _, d := range []string{cfg.Home, filepath.Join(cfg.Home, "servers"), filepath.Join(cfg.Home, "logs")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("daemon: create %s: %w", d, err)
		}
	}
	issuer, err := token.NewIssuer(filepath.Join(cfg.Home, "keys", "signing.pem"), Issuer, nil)
	if err != nil {
		return nil, err
	}
	auditLog, err := audit.NewLog(filepath.Join(cfg.Home, "audit.jsonl"), 0)
	if err != nil {
		return nil, err
	}
	base := "http://" + cfg.Addr
	run := &runner.Runner{LogDir: filepath.Join(cfg.Home, "logs")}
	svc := hub.New(hub.Config{
		Catalogue: cfg.Catalogue,
		Installer: &registry.Installer{Dir: filepath.Join(cfg.Home, "servers"), Client: cfg.RegistryClient},
		Runner:    hub.RunnerFrom(run),
		Claude: &claudeconfig.Writer{Paths: claudeconfig.Paths{
			ClaudeConfig: cfg.ClaudeConfigPath,
			Ledger:       filepath.Join(cfg.Home, "state.json"),
			BackupDir:    filepath.Join(cfg.Home, "backups"),
		}},
		ProxyBase: base,
		Issuer:    Issuer,
		Logger:    cfg.Logger,
	})
	var skill server.Skill
	if cfg.ClaudeDir != "" {
		skill = skillfiles.Manager{ClaudeDir: cfg.ClaudeDir}
	}
	h := server.New(server.Options{
		Version:   cfg.Version,
		UI:        cfg.UI,
		Catalogue: cfg.Catalogue,
		State:     svc.State,
		Servers:   svc,
		Proxy:     proxy.New(proxy.Config{Resolver: svc, Tokens: issuer, Audit: auditLog, User: cfg.User}),
		JWKS:      issuer.JWKSHandler(),
		Audit:     auditLog,
		Skill:     skill,
		Status: func() server.Status {
			return server.Status{
				Version: cfg.Version, Addr: cfg.Addr, ClaudeConfigPath: cfg.ClaudeConfigPath,
				EnabledCount: svc.EnabledCount(), CatalogueSource: cfg.CatalogueSource, User: cfg.User,
			}
		},
	})
	return &Daemon{Hub: svc, handler: h, runner: run, audit: auditLog, logger: cfg.Logger}, nil
}

// Handler is the root HTTP handler.
func (d *Daemon) Handler() http.Handler { return d.handler }

// Restore re-establishes previously enabled servers; failures are logged and
// reported per server in their state, never fatal.
func (d *Daemon) Restore(ctx context.Context) {
	if err := d.Hub.Restore(ctx); err != nil {
		d.logger.Warn("restore incomplete", "err", err)
	}
}

// Close shuts down in an order that leaves no server process behind: refuse
// new enables, wait (bounded) for in-flight ones, stop every process (the
// runner also aborts any start still running), then close the audit log.
// Safe to call more than once.
func (d *Daemon) Close() error {
	d.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), DrainTimeout)
		defer cancel()
		if err := d.Hub.Close(ctx); err != nil {
			d.logger.Warn("in-flight enables still running at shutdown; stopping processes anyway", "err", err)
		}
		d.runner.StopAll()
		d.closeErr = d.audit.Close()
	})
	return d.closeErr
}
