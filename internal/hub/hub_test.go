package hub_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
	"github.com/Arif-Karim/mcp-hub/internal/claudeconfig"
	"github.com/Arif-Karim/mcp-hub/internal/hub"
	"github.com/Arif-Karim/mcp-hub/internal/registry"
	"github.com/Arif-Karim/mcp-hub/internal/runner"
)

const (
	base     = "http://127.0.0.1:7420"
	platform = "testos-testarch"
)

var t0 = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

// --- fakes ---

type fakeInstaller struct {
	mu    sync.Mutex
	dir   string
	err   error
	calls []string // "id version url sha"
}

func (f *fakeInstaller) Install(_ context.Context, id, version string, a catalogue.Artifact) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("%s %s %s %s", id, version, a.URL, a.SHA256))
	if f.err != nil {
		return "", f.err
	}
	return filepath.Join(f.dir, id, version), nil
}

type fakeProc struct {
	mu      sync.Mutex
	url     string
	running bool
	err     error
}

func (p *fakeProc) URL() string { return p.url }
func (p *fakeProc) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}
func (p *fakeProc) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}
func (p *fakeProc) die(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running, p.err = false, err
}

type startCall struct {
	id, exe string
	env     map[string]string
}

type fakeRunner struct {
	mu       sync.Mutex
	err      error
	delay    time.Duration
	starts   []startCall
	stops    []string
	procs    map[string]*fakeProc
	nextPort int
}

func (f *fakeRunner) Start(_ context.Context, id, exe string, env map[string]string) (hub.Process, error) {
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, startCall{id, exe, env})
	if f.err != nil {
		return nil, f.err
	}
	f.nextPort++
	p := &fakeProc{url: fmt.Sprintf("http://127.0.0.1:%d", 50000+f.nextPort), running: true}
	if f.procs == nil {
		f.procs = map[string]*fakeProc{}
	}
	f.procs[id] = p
	return p, nil
}

func (f *fakeRunner) Stop(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops = append(f.stops, id)
	if p, ok := f.procs[id]; ok {
		p.die(nil)
	}
	return nil
}

func (f *fakeRunner) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.starts)
}

// --- fixtures ---

func entry(id string, kind catalogue.Kind, approval catalogue.Approval) catalogue.Entry {
	e := catalogue.Entry{
		ID: id, Name: id, Description: "d", Owner: "o", Version: "1.2.3", Kind: kind,
		Classification: catalogue.ClassInternal, Approval: approval, Auth: catalogue.AuthHubJWT,
	}
	if kind == catalogue.KindRemote {
		e.Auth, e.URL = catalogue.AuthNone, "https://mcp."+id+".test/mcp"
	} else {
		e.Artifacts = map[string]catalogue.Artifact{platform: {URL: "https://reg.test/" + id + ".tgz", SHA256: "abc123"}}
	}
	return e
}

func store(entries ...catalogue.Entry) *catalogue.Store {
	return catalogue.NewStore(&catalogue.Catalogue{Version: 1, Entries: entries})
}

func defaultStore() *catalogue.Store {
	return store(
		entry("wiki", catalogue.KindRemote, catalogue.ApprovalApproved),
		entry("svc", catalogue.KindLocal, catalogue.ApprovalApproved),
		entry("pending-one", catalogue.KindRemote, catalogue.ApprovalPending),
		entry("revoked-one", catalogue.KindLocal, catalogue.ApprovalRevoked),
	)
}

type env struct {
	dir    string
	writer *claudeconfig.Writer
	inst   *fakeInstaller
	run    *fakeRunner
	now    time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{dir: dir, inst: &fakeInstaller{dir: filepath.Join(dir, "servers")}, run: &fakeRunner{}, now: t0}
	e.writer = &claudeconfig.Writer{Paths: claudeconfig.Paths{
		ClaudeConfig: filepath.Join(dir, "claude.json"),
		Ledger:       filepath.Join(dir, "hub", "state.json"),
		BackupDir:    filepath.Join(dir, "hub", "backups"),
	}}
	e.writer.Now = func() time.Time { return e.now }
	return e
}

func (e *env) service(s *catalogue.Store) *hub.Service {
	return hub.New(hub.Config{
		Catalogue: s, Installer: e.inst, Runner: e.run, Claude: e.writer,
		ProxyBase: base, Issuer: "mcp-hub", Platform: platform, Logger: slog.New(slog.DiscardHandler),
		Now: func() time.Time { return e.now },
	})
}

// servers returns the mcpServers map from the claude config (nil if no file).
func (e *env) servers(t *testing.T) map[string]claudeconfig.ServerEntry {
	t.Helper()
	data, err := os.ReadFile(e.writer.ClaudeConfig)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MCPServers map[string]claudeconfig.ServerEntry `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.MCPServers
}

func (e *env) writeConfig(t *testing.T, doc string) {
	t.Helper()
	if err := os.WriteFile(e.writer.ClaudeConfig, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
}

// --- tests ---

func TestEnableRemote(t *testing.T) {
	e := newEnv(t)
	s := e.service(defaultStore())

	st, err := s.Enable(context.Background(), "wiki")
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	want := catalogue.InstallState{
		Enabled: true, Status: catalogue.StatusReady, ConfigName: "hub__wiki",
		ProxyURL: base + "/mcp/wiki", EnabledAt: t0,
	}
	if st != want {
		t.Fatalf("state = %+v\nwant    %+v", st, want)
	}
	if got := e.servers(t)["hub__wiki"]; got != (claudeconfig.ServerEntry{Type: "http", URL: base + "/mcp/wiki"}) {
		t.Fatalf("config entry = %+v", got)
	}
	up, ok := s.Resolve("wiki")
	if !ok || up.URL != "https://mcp.wiki.test/mcp" || up.Auth != catalogue.AuthNone {
		t.Fatalf("Resolve = %+v, %v", up, ok)
	}
	if n := s.EnabledCount(); n != 1 {
		t.Fatalf("EnabledCount = %d, want 1", n)
	}
	if e.run.startCount() != 0 || len(e.inst.calls) != 0 {
		t.Fatal("remote enable must not install or start anything")
	}

	// Idempotent: a second Enable keeps the original enabledAt.
	e.now = t0.Add(time.Hour)
	st2, err := s.Enable(context.Background(), "wiki")
	if err != nil || st2 != want {
		t.Fatalf("second Enable = %+v, %v; want %+v", st2, err, want)
	}
}

func TestEnableRefusesUnapproved(t *testing.T) {
	for _, id := range []string{"pending-one", "revoked-one"} {
		t.Run(id, func(t *testing.T) {
			e := newEnv(t)
			s := e.service(defaultStore())
			st, err := s.Enable(context.Background(), id)
			if !errors.Is(err, catalogue.ErrNotApproved) {
				t.Fatalf("err = %v, want ErrNotApproved", err)
			}
			if st.Enabled || st.Status != catalogue.StatusDisabled || st.ConfigName != "hub__"+id {
				t.Fatalf("state = %+v", st)
			}
			if e.servers(t) != nil {
				t.Fatal("claude config written for an unapproved entry")
			}
			if len(e.inst.calls) != 0 || e.run.startCount() != 0 {
				t.Fatal("unapproved entry was installed or started")
			}
			if _, ok := s.Resolve(id); ok {
				t.Fatal("unapproved entry resolves")
			}
		})
	}
}

func TestEnableUnknown(t *testing.T) {
	e := newEnv(t)
	s := e.service(defaultStore())
	if _, err := s.Enable(context.Background(), "nope"); !errors.Is(err, catalogue.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if e.servers(t) != nil {
		t.Fatal("config written")
	}
}

func TestEnableLocal(t *testing.T) {
	e := newEnv(t)
	s := e.service(defaultStore())

	st, err := s.Enable(context.Background(), "svc")
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if st.Status != catalogue.StatusRunning || !st.Enabled || st.ProxyURL != base+"/mcp/svc" {
		t.Fatalf("state = %+v", st)
	}
	if want := []string{"svc 1.2.3 https://reg.test/svc.tgz abc123"}; fmt.Sprint(e.inst.calls) != fmt.Sprint(want) {
		t.Fatalf("install calls = %v, want %v", e.inst.calls, want)
	}
	if len(e.run.starts) != 1 {
		t.Fatalf("starts = %d", len(e.run.starts))
	}
	sc := e.run.starts[0]
	if sc.id != "svc" || sc.exe != filepath.Join(e.inst.dir, "svc", "1.2.3", "mcp-server") {
		t.Fatalf("start = %+v", sc)
	}
	wantEnv := map[string]string{"HUB_JWKS_URL": base + "/.well-known/jwks.json", "HUB_AUDIENCE": "svc", "HUB_ISSUER": "mcp-hub"}
	if fmt.Sprint(sc.env) != fmt.Sprint(wantEnv) {
		t.Fatalf("env = %v, want %v", sc.env, wantEnv)
	}
	up, ok := s.Resolve("svc")
	if !ok || up.URL != e.run.procs["svc"].url+"/mcp" || up.Auth != catalogue.AuthHubJWT {
		t.Fatalf("Resolve = %+v, %v", up, ok)
	}
	if e.servers(t)["hub__svc"].URL != base+"/mcp/svc" {
		t.Fatalf("config = %+v", e.servers(t))
	}

	// Enable while running: no second start.
	if _, err := s.Enable(context.Background(), "svc"); err != nil || e.run.startCount() != 1 {
		t.Fatalf("re-enable: err %v, starts %d", err, e.run.startCount())
	}

	// The process crashes: status error with the exit reason, no longer resolves.
	e.run.procs["svc"].die(errors.New("exit status 3"))
	st = s.State("svc")
	if st.Status != catalogue.StatusError || st.LastError != "server exited: exit status 3" || !st.Enabled {
		t.Fatalf("after crash state = %+v", st)
	}
	if _, ok := s.Resolve("svc"); ok {
		t.Fatal("crashed server still resolves")
	}

	// Enable again restarts it.
	st, err = s.Enable(context.Background(), "svc")
	if err != nil || st.Status != catalogue.StatusRunning || e.run.startCount() != 2 {
		t.Fatalf("restart: %+v %v starts=%d", st, err, e.run.startCount())
	}

	// A clean exit reads as stopped.
	e.run.procs["svc"].die(nil)
	if st := s.State("svc"); st.Status != catalogue.StatusStopped || st.LastError != "" {
		t.Fatalf("after clean exit state = %+v", st)
	}
}

func TestEnableLocalFailuresWriteNothing(t *testing.T) {
	cases := map[string]struct {
		instErr, runErr error
		platform        string
		want            error
		wantStarts      int
	}{
		"checksum mismatch":    {instErr: fmt.Errorf("%w: got x", registry.ErrChecksumMismatch), want: registry.ErrChecksumMismatch},
		"registry down":        {instErr: registry.ErrRegistryUnavailable, want: registry.ErrRegistryUnavailable},
		"unsupported platform": {platform: "plan9-mips", want: registry.ErrUnsupportedPlatform},
		"never healthy":        {runErr: fmt.Errorf("%w: boom", runner.ErrStartFailed), want: runner.ErrStartFailed, wantStarts: 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.inst.err, e.run.err = tc.instErr, tc.runErr
			p := platform
			if tc.platform != "" {
				p = tc.platform
			}
			s := hub.New(hub.Config{
				Catalogue: defaultStore(), Installer: e.inst, Runner: e.run, Claude: e.writer,
				ProxyBase: base, Platform: p,
			})
			st, err := s.Enable(context.Background(), "svc")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if e.run.startCount() != tc.wantStarts {
				t.Fatalf("starts = %d, want %d", e.run.startCount(), tc.wantStarts)
			}
			if e.servers(t) != nil {
				t.Fatal("claude config written after a failed install/start")
			}
			if st.Enabled || st.Status != catalogue.StatusDisabled || st.LastError != err.Error() {
				t.Fatalf("state = %+v, want disabled with lastError %q", st, err)
			}
			if _, ok := s.Resolve("svc"); ok {
				t.Fatal("failed server resolves")
			}
		})
	}
}

func TestEnableConfigConflictStopsProcess(t *testing.T) {
	e := newEnv(t)
	// The user already has their own hub__svc: the hub must not overwrite it.
	e.writeConfig(t, `{"mcpServers":{"hub__svc":{"type":"http","url":"http://mine"}}}`)
	s := e.service(defaultStore())

	_, err := s.Enable(context.Background(), "svc")
	if !errors.Is(err, claudeconfig.ErrConfigConflict) {
		t.Fatalf("err = %v, want ErrConfigConflict", err)
	}
	if fmt.Sprint(e.run.stops) != "[svc]" {
		t.Fatalf("stops = %v, want the freshly started process stopped", e.run.stops)
	}
	if e.run.procs["svc"].Running() {
		t.Fatal("process left running")
	}
	if e.servers(t)["hub__svc"].URL != "http://mine" {
		t.Fatal("user's entry was changed")
	}
	if st := s.State("svc"); st.Enabled {
		t.Fatalf("state = %+v", st)
	}
}

func TestReEnableConflictKeepsRunningServer(t *testing.T) {
	e := newEnv(t)
	s := e.service(defaultStore())
	if _, err := s.Enable(context.Background(), "svc"); err != nil {
		t.Fatal(err)
	}
	e.writeConfig(t, `{"mcpServers":{"hub__svc":{"type":"http","url":"http://edited"}}}`)
	if _, err := s.Enable(context.Background(), "svc"); !errors.Is(err, claudeconfig.ErrConfigConflict) {
		t.Fatalf("err = %v, want ErrConfigConflict", err)
	}
	// The process was already running before this call: it is not ours to roll back.
	if len(e.run.stops) != 0 || !e.run.procs["svc"].Running() {
		t.Fatal("already-running server was stopped by a failed re-enable")
	}
	if st := s.State("svc"); st.Status != catalogue.StatusRunning {
		t.Fatalf("state = %+v, want still running", st)
	}
	if _, ok := s.Resolve("svc"); !ok {
		t.Fatal("running server stopped resolving after a failed re-enable")
	}
}

func TestEnableConfigInvalid(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(t, `not json`)
	s := e.service(defaultStore())
	if _, err := s.Enable(context.Background(), "wiki"); !errors.Is(err, claudeconfig.ErrConfigInvalid) {
		t.Fatalf("err = %v, want ErrConfigInvalid", err)
	}
}

func TestDisable(t *testing.T) {
	e := newEnv(t)
	s := e.service(defaultStore())
	for _, id := range []string{"wiki", "svc"} {
		if _, err := s.Enable(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	st, err := s.Disable(context.Background(), "svc")
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	want := catalogue.InstallState{Status: catalogue.StatusDisabled, ConfigName: "hub__svc"}
	if st != want {
		t.Fatalf("state = %+v, want %+v", st, want)
	}
	if _, ok := e.servers(t)["hub__svc"]; ok {
		t.Fatal("config entry not removed")
	}
	if _, ok := e.servers(t)["hub__wiki"]; !ok {
		t.Fatal("other entry removed")
	}
	if e.run.procs["svc"].Running() {
		t.Fatal("process not stopped")
	}
	if _, ok := s.Resolve("svc"); ok {
		t.Fatal("disabled server resolves")
	}
	if n := s.EnabledCount(); n != 1 {
		t.Fatalf("EnabledCount = %d, want 1", n)
	}
	// Idempotent.
	if st, err := s.Disable(context.Background(), "svc"); err != nil || st != want {
		t.Fatalf("second Disable = %+v, %v", st, err)
	}
}

func TestDisableConflictLeavesServerRunning(t *testing.T) {
	e := newEnv(t)
	s := e.service(defaultStore())
	if _, err := s.Enable(context.Background(), "svc"); err != nil {
		t.Fatal(err)
	}
	// The user edits the hub entry by hand.
	e.writeConfig(t, `{"mcpServers":{"hub__svc":{"type":"http","url":"http://edited"}}}`)
	_, err := s.Disable(context.Background(), "svc")
	if !errors.Is(err, claudeconfig.ErrConfigConflict) {
		t.Fatalf("err = %v, want ErrConfigConflict", err)
	}
	if len(e.run.stops) != 0 || !e.run.procs["svc"].Running() {
		t.Fatal("server stopped although its config entry was left in place")
	}
	if st := s.State("svc"); !st.Enabled || st.Status != catalogue.StatusRunning {
		t.Fatalf("state = %+v", st)
	}
}

func TestRestore(t *testing.T) {
	e := newEnv(t)
	first := e.service(store(
		entry("wiki", catalogue.KindRemote, catalogue.ApprovalApproved),
		entry("svc", catalogue.KindLocal, catalogue.ApprovalApproved),
		entry("legacy", catalogue.KindLocal, catalogue.ApprovalApproved),
		entry("gone", catalogue.KindRemote, catalogue.ApprovalApproved),
		entry("paused", catalogue.KindRemote, catalogue.ApprovalApproved),
	))
	for _, id := range []string{"wiki", "svc", "legacy", "gone", "paused"} {
		if _, err := first.Enable(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}

	// Daemon restart: the platform team revoked legacy, delisted gone and
	// moved paused back to pending.
	e.run = &fakeRunner{}
	e.now = t0.Add(24 * time.Hour)
	s := e.service(store(
		entry("wiki", catalogue.KindRemote, catalogue.ApprovalApproved),
		entry("svc", catalogue.KindLocal, catalogue.ApprovalApproved),
		entry("legacy", catalogue.KindLocal, catalogue.ApprovalRevoked),
		entry("paused", catalogue.KindRemote, catalogue.ApprovalPending),
	))
	if err := s.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	cfg := e.servers(t)
	for id, want := range map[string]bool{"wiki": true, "svc": true, "legacy": false, "gone": false, "paused": false} {
		if _, ok := cfg["hub__"+id]; ok != want {
			t.Errorf("hub__%s in config = %v, want %v", id, ok, want)
		}
		if _, ok := s.Resolve(id); ok != want {
			t.Errorf("Resolve(%s) = %v, want %v", id, ok, want)
		}
	}
	if st := s.State("legacy"); st.Enabled || st.Status != catalogue.StatusDisabled || st.LastError != "disabled: revoked by platform team" {
		t.Errorf("legacy state = %+v", st)
	}
	if st := s.State("gone"); st.Enabled || st.LastError != "disabled: removed from the catalogue" {
		t.Errorf("gone state = %+v", st)
	}
	if st := s.State("paused"); st.Enabled || st.LastError != "disabled: approval is pending" {
		t.Errorf("paused state = %+v", st)
	}
	if st := s.State("wiki"); st.Status != catalogue.StatusReady || !st.EnabledAt.Equal(t0) {
		t.Errorf("wiki state = %+v, want ready with the original enabledAt", st)
	}
	if st := s.State("svc"); st.Status != catalogue.StatusRunning || !st.EnabledAt.Equal(t0) {
		t.Errorf("svc state = %+v", st)
	}
	if e.run.startCount() != 1 || e.run.starts[0].id != "svc" {
		t.Errorf("starts = %+v, want only svc restarted", e.run.starts)
	}
	if n := s.EnabledCount(); n != 2 {
		t.Errorf("EnabledCount = %d, want 2", n)
	}
}

func TestRestoreFailures(t *testing.T) {
	e := newEnv(t)
	first := e.service(defaultStore())
	if _, err := first.Enable(context.Background(), "svc"); err != nil {
		t.Fatal(err)
	}
	e.run = &fakeRunner{err: fmt.Errorf("%w: port busy", runner.ErrStartFailed)}
	s := e.service(defaultStore())
	err := s.Restore(context.Background())
	if !errors.Is(err, runner.ErrStartFailed) || !strings.Contains(err.Error(), "restore svc") {
		t.Fatalf("Restore err = %v", err)
	}
	st := s.State("svc")
	if !st.Enabled || st.Status != catalogue.StatusError || !strings.Contains(st.LastError, "port busy") {
		t.Fatalf("state = %+v, want enabled/error with the cause", st)
	}
	if _, ok := s.Resolve("svc"); ok {
		t.Fatal("failed restore resolves")
	}
	// Enable recovers it once the cause is gone.
	e.run.err = nil
	if st, err := s.Enable(context.Background(), "svc"); err != nil || st.Status != catalogue.StatusRunning || st.LastError != "" {
		t.Fatalf("recover: %+v %v", st, err)
	}
}

func TestRestoreRevokedButUserEdited(t *testing.T) {
	e := newEnv(t)
	first := e.service(store(entry("wiki", catalogue.KindRemote, catalogue.ApprovalApproved)))
	if _, err := first.Enable(context.Background(), "wiki"); err != nil {
		t.Fatal(err)
	}
	e.writeConfig(t, `{"mcpServers":{"hub__wiki":{"type":"http","url":"http://edited"}}}`)
	s := e.service(store(entry("wiki", catalogue.KindRemote, catalogue.ApprovalRevoked)))
	err := s.Restore(context.Background())
	if !errors.Is(err, claudeconfig.ErrConfigConflict) {
		t.Fatalf("err = %v, want ErrConfigConflict", err)
	}
	st := s.State("wiki")
	if st.Enabled || !strings.HasPrefix(st.LastError, "disabled: revoked by platform team (config not removed") {
		t.Fatalf("state = %+v", st)
	}
	if _, ok := s.Resolve("wiki"); ok {
		t.Fatal("revoked server resolves")
	}
}

func TestRestoreLedgerUnreadable(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(filepath.Dir(e.writer.Ledger), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.writer.Ledger, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.service(defaultStore()).Restore(context.Background()); err == nil || !strings.Contains(err.Error(), "read ledger") {
		t.Fatalf("err = %v", err)
	}
}

func TestConcurrentEnableStartsOnce(t *testing.T) {
	e := newEnv(t)
	e.run.delay = 50 * time.Millisecond
	s := e.service(defaultStore())
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, err := s.Enable(context.Background(), "svc")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := e.run.startCount(); n != 1 {
		t.Fatalf("started %d times, want 1", n)
	}
}

func TestDefaults(t *testing.T) {
	e := newEnv(t)
	s := hub.New(hub.Config{
		Catalogue: store(entry("svc", catalogue.KindLocal, catalogue.ApprovalApproved)),
		Installer: e.inst, Runner: e.run, Claude: e.writer, ProxyBase: base, JWKSURL: "https://idp.test/jwks",
	})
	// Default platform is this machine's, which the fixture does not ship.
	if _, err := s.Enable(context.Background(), "svc"); !errors.Is(err, registry.ErrUnsupportedPlatform) {
		t.Fatalf("err = %v, want ErrUnsupportedPlatform for %s", err, registry.PlatformKey())
	}
	s = hub.New(hub.Config{
		Catalogue: store(entry("svc", catalogue.KindLocal, catalogue.ApprovalApproved)),
		Installer: e.inst, Runner: e.run, Claude: e.writer, ProxyBase: base, JWKSURL: "https://idp.test/jwks", Platform: platform,
	})
	if _, err := s.Enable(context.Background(), "svc"); err != nil {
		t.Fatal(err)
	}
	if got := e.run.starts[0].env["HUB_JWKS_URL"]; got != "https://idp.test/jwks" {
		t.Fatalf("HUB_JWKS_URL = %q", got)
	}
	if st := s.State("svc"); st.EnabledAt.IsZero() {
		t.Fatal("default clock not used")
	}
}

func TestRunnerFrom(t *testing.T) {
	r := hub.RunnerFrom(&runner.Runner{LogDir: t.TempDir()})
	if _, err := r.Start(context.Background(), "x1", filepath.Join(t.TempDir(), "missing"), nil); !errors.Is(err, runner.ErrStartFailed) {
		t.Fatalf("err = %v, want ErrStartFailed", err)
	}
	if err := r.Stop("x1"); err != nil {
		t.Fatal(err)
	}
}
