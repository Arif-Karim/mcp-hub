// Package hub orchestrates enabling and disabling catalogue entries: the
// governance check, installing and starting local servers, writing the
// hub-owned Claude Code config entry, and answering the proxy's "where does
// this id live" question. It is the only place those steps are sequenced.
package hub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/claudeconfig"
	"github.com/Arif-Karim/mcp-hub/internal/proxy"
	"github.com/Arif-Karim/mcp-hub/internal/registry"
	"github.com/Arif-Karim/mcp-hub/internal/runner"
)

// ExecutableName is the file every local server archive must contain.
const ExecutableName = "mcp-server"

// Installer downloads, verifies and unpacks a local server (registry.Installer).
type Installer interface {
	Install(ctx context.Context, id, version string, a catalogue.Artifact) (string, error)
}

// Process is a started local server.
type Process interface {
	URL() string // base URL, e.g. http://127.0.0.1:54321
	Running() bool
	Err() error // exit error once the process has died
}

// Runner starts and stops local servers (see RunnerFrom).
type Runner interface {
	Start(ctx context.Context, id, exePath string, env map[string]string) (Process, error)
	Stop(id string) error
}

// ConfigWriter edits Claude Code's config (claudeconfig.Writer).
type ConfigWriter interface {
	Add(id string, e claudeconfig.ServerEntry) error
	Remove(id string) error
	Owned() (map[string]claudeconfig.Owned, error)
}

// Config configures New.
type Config struct {
	Catalogue *catalogue.Store
	Installer Installer
	Runner    Runner
	Claude    ConfigWriter
	ProxyBase string // daemon base URL, e.g. http://127.0.0.1:7420
	JWKSURL   string // passed to local servers; "": ProxyBase + /.well-known/jwks.json
	Issuer    string // passed to local servers as HUB_ISSUER
	Platform  string // artifact key; "": registry.PlatformKey()
	Logger    *slog.Logger
	Now       func() time.Time
}

// record is the in-memory state of one id.
type record struct {
	enabled   bool
	upstream  proxy.Upstream
	proc      Process // local only
	enabledAt time.Time
	lastError string
}

// Service is the enable/disable orchestrator. It implements proxy.Resolver.
type Service struct {
	cfg Config

	mu      sync.Mutex // guards records and locks
	records map[string]*record
	locks   map[string]*sync.Mutex
}

// New returns a Service. Call Restore once at startup.
func New(cfg Config) *Service {
	if cfg.JWKSURL == "" {
		cfg.JWKSURL = cfg.ProxyBase + "/.well-known/jwks.json"
	}
	if cfg.Platform == "" {
		cfg.Platform = registry.PlatformKey()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{cfg: cfg, records: map[string]*record{}, locks: map[string]*sync.Mutex{}}
}

// ProxyURL is what Claude Code is pointed at for id.
func (s *Service) ProxyURL(id string) string { return s.cfg.ProxyBase + "/mcp/" + id }

// Enable makes id usable from Claude Code. It is idempotent.
func (s *Service) Enable(ctx context.Context, id string) (catalogue.InstallState, error) {
	defer s.lock(id)()
	e, err := s.cfg.Catalogue.Get(id)
	if err != nil {
		return s.State(id), err
	}
	if err := catalogue.CanEnable(e); err != nil {
		return s.State(id), err
	}
	if err := s.enable(ctx, e, s.cfg.Now()); err != nil {
		// A server that was already enabled and healthy keeps serving: the
		// failed call changed nothing it depends on. Otherwise show why.
		if _, healthy := s.Resolve(id); !healthy {
			s.update(id, func(r *record) { r.lastError = err.Error() })
		}
		return s.State(id), err
	}
	s.cfg.Logger.Info("server enabled", "server", id, "kind", string(e.Kind))
	return s.State(id), nil
}

// enable brings e up (installing/starting a local server if it is not already
// running) and writes its config entry. A config failure after a fresh start
// stops the process again, so a failed Enable leaves nothing running.
func (s *Service) enable(ctx context.Context, e catalogue.Entry, enabledAt time.Time) error {
	up := proxy.Upstream{URL: e.URL, Auth: e.Auth}
	var proc Process
	started := false
	if e.Kind == catalogue.KindLocal {
		if r := s.get(e.ID); r.proc != nil && r.proc.Running() {
			proc = r.proc
		} else {
			p, err := s.start(ctx, e)
			if err != nil {
				return err
			}
			proc, started = p, true
		}
		up.URL = proc.URL() + "/mcp"
	}
	if err := s.cfg.Claude.Add(e.ID, claudeconfig.ServerEntry{Type: "http", URL: s.ProxyURL(e.ID)}); err != nil {
		if started {
			_ = s.cfg.Runner.Stop(e.ID)
		}
		return err
	}
	s.update(e.ID, func(r *record) {
		if !r.enabled {
			r.enabledAt = enabledAt
		}
		r.enabled, r.upstream, r.proc, r.lastError = true, up, proc, ""
	})
	return nil
}

func (s *Service) start(ctx context.Context, e catalogue.Entry) (Process, error) {
	art, err := registry.Select(e, s.cfg.Platform)
	if err != nil {
		return nil, err
	}
	dir, err := s.cfg.Installer.Install(ctx, e.ID, e.Version, art)
	if err != nil {
		return nil, err
	}
	return s.cfg.Runner.Start(ctx, e.ID, filepath.Join(dir, ExecutableName), map[string]string{
		"HUB_JWKS_URL": s.cfg.JWKSURL,
		"HUB_AUDIENCE": e.ID,
		"HUB_ISSUER":   s.cfg.Issuer,
	})
}

// Disable removes id's config entry, then stops its process. If the config
// entry cannot be removed (the user edited it) nothing is stopped. Idempotent.
func (s *Service) Disable(_ context.Context, id string) (catalogue.InstallState, error) {
	defer s.lock(id)()
	if err := s.disable(id, ""); err != nil {
		return s.State(id), err
	}
	s.cfg.Logger.Info("server disabled", "server", id)
	return s.State(id), nil
}

func (s *Service) disable(id, notice string) error {
	if err := s.cfg.Claude.Remove(id); err != nil {
		return err
	}
	if err := s.cfg.Runner.Stop(id); err != nil {
		return err
	}
	s.update(id, func(r *record) { *r = record{lastError: notice} })
	return nil
}

// Restore re-establishes every hub-owned entry at daemon startup. Entries
// that are gone from the catalogue or no longer approved are disabled
// (governance wins over the developer's earlier choice); approved local
// servers are restarted; approved remotes are ready.
func (s *Service) Restore(ctx context.Context) error {
	owned, err := s.cfg.Claude.Owned()
	if err != nil {
		return fmt.Errorf("read ledger: %w", err)
	}
	var errs []error
	for _, id := range slices.Sorted(maps.Keys(owned)) {
		if err := s.restore(ctx, id, owned[id].EnabledAt); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) restore(ctx context.Context, id string, enabledAt time.Time) error {
	defer s.lock(id)()
	e, err := s.cfg.Catalogue.Get(id)
	reason := ""
	switch {
	case err != nil:
		reason = "disabled: removed from the catalogue"
	case e.Approval == catalogue.ApprovalRevoked:
		reason = "disabled: revoked by platform team"
	case e.Approval != catalogue.ApprovalApproved:
		reason = "disabled: approval is " + string(e.Approval)
	}
	if reason != "" {
		s.cfg.Logger.Warn("auto-disabling server", "server", id, "reason", reason)
		if err := s.disable(id, reason); err != nil {
			s.update(id, func(r *record) { r.lastError = reason + " (config not removed: " + err.Error() + ")" })
			return fmt.Errorf("auto-disable %s: %w", id, err)
		}
		return nil
	}
	if err := s.enable(ctx, e, enabledAt); err != nil {
		s.cfg.Logger.Error("restore failed", "server", id, "err", err)
		s.update(id, func(r *record) { r.enabled, r.enabledAt, r.lastError = true, enabledAt, err.Error() })
		return fmt.Errorf("restore %s: %w", id, err)
	}
	s.cfg.Logger.Info("server restored", "server", id)
	return nil
}

// State is id's install state for the catalogue API.
func (s *Service) State(id string) catalogue.InstallState {
	r := s.get(id)
	st := catalogue.InstallState{Status: catalogue.StatusDisabled, ConfigName: catalogue.ConfigName(id), LastError: r.lastError}
	if !r.enabled {
		return st
	}
	st.Enabled, st.ProxyURL, st.EnabledAt = true, s.ProxyURL(id), r.enabledAt
	switch {
	case r.lastError != "":
		st.Status = catalogue.StatusError
	case r.proc == nil:
		st.Status = catalogue.StatusReady
	case r.proc.Running():
		st.Status = catalogue.StatusRunning
	case r.proc.Err() != nil:
		st.Status, st.LastError = catalogue.StatusError, "server exited: "+r.proc.Err().Error()
	default:
		st.Status = catalogue.StatusStopped
	}
	return st
}

// Resolve implements proxy.Resolver: only enabled, ready or running servers resolve.
func (s *Service) Resolve(id string) (proxy.Upstream, bool) {
	r := s.get(id)
	if !r.enabled || r.lastError != "" || r.upstream.URL == "" || (r.proc != nil && !r.proc.Running()) {
		return proxy.Upstream{}, false
	}
	return r.upstream, true
}

// EnabledCount is the number of enabled entries (for /api/v1/status).
func (s *Service) EnabledCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.records {
		if r.enabled {
			n++
		}
	}
	return n
}

// get returns a copy of id's record (zero if unknown).
func (s *Service) get(id string) record {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.records[id]; ok {
		return *r
	}
	return record{}
}

func (s *Service) update(id string, f func(*record)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		r = &record{}
		s.records[id] = r
	}
	f(r)
}

// lock serialises Enable/Disable/Restore of one id and returns the unlock func.
func (s *Service) lock(id string) func() {
	s.mu.Lock()
	l, ok := s.locks[id]
	if !ok {
		l = &sync.Mutex{}
		s.locks[id] = l
	}
	s.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// RunnerFrom adapts a *runner.Runner to the Runner interface.
func RunnerFrom(r *runner.Runner) Runner { return procRunner{r} }

type procRunner struct{ r *runner.Runner }

func (p procRunner) Start(ctx context.Context, id, exe string, env map[string]string) (Process, error) {
	proc, err := p.r.Start(ctx, id, exe, env)
	if err != nil {
		return nil, err
	}
	return proc, nil
}

func (p procRunner) Stop(id string) error { return p.r.Stop(id) }
