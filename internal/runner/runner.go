// Package runner starts and supervises local MCP server processes: each gets
// a free loopback port, a minimal environment, its own process group and a
// 0600 log file, and is only reported started once GET /healthz answers 200.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrStartFailed (contract code start_failed): the process could not be
// started or never became healthy.
var ErrStartFailed = errors.New("mcp server failed to start")

// Defaults for Runner.
const (
	DefaultHealthTimeout = 10 * time.Second
	DefaultStopTimeout   = 5 * time.Second
	pollInterval         = 50 * time.Millisecond
	logTailBytes         = 2048
)

// Runner owns the local server processes, one per id.
type Runner struct {
	LogDir        string        // <MCP_HUB_HOME>/logs
	HealthTimeout time.Duration // <= 0: DefaultHealthTimeout
	StopTimeout   time.Duration // <= 0: DefaultStopTimeout

	mu    sync.Mutex
	procs map[string]*Proc
}

// Proc is one started server process.
type Proc struct {
	port int
	cmd  *exec.Cmd
	done chan struct{} // closed when the process has exited

	mu  sync.Mutex
	err error // exit error, set before done is closed
}

// Port is the loopback port the server listens on.
func (p *Proc) Port() int { return p.port }

// URL is the server's base URL, e.g. http://127.0.0.1:54321.
func (p *Proc) URL() string { return "http://127.0.0.1:" + strconv.Itoa(p.port) }

// Running reports whether the process is still alive.
func (p *Proc) Running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// Err is the exit error once the process has exited (nil while running or
// after a clean exit).
func (p *Proc) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// Get returns the tracked process for id.
func (r *Runner) Get(id string) (*Proc, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.procs[id]
	return p, ok
}

// Start launches exePath (cwd = its directory) with PATH, HOME, env and PORT,
// and waits until it is healthy. A process already running for id is returned
// as is. On failure the process is killed and the error wraps ErrStartFailed
// with the tail of its log. ctx bounds only the health wait, not the process.
func (r *Runner) Start(ctx context.Context, id, exePath string, env map[string]string) (*Proc, error) {
	if !safeID(id) {
		return nil, fmt.Errorf("%w: invalid id %q", ErrStartFailed, id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.procs[id]; ok && p.Running() {
		return p, nil
	}
	delete(r.procs, id)

	if err := os.MkdirAll(r.LogDir, 0o700); err != nil {
		return nil, fmt.Errorf("%w: log dir: %w", ErrStartFailed, err)
	}
	logPath := filepath.Join(r.LogDir, id+".log")
	logf, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600) // #nosec G304 -- id validated by safeID
	if err != nil {
		return nil, fmt.Errorf("%w: log file: %w", ErrStartFailed, err)
	}
	port, err := freePort(ctx)
	if err != nil {
		_ = logf.Close()
		return nil, fmt.Errorf("%w: %w", ErrStartFailed, err)
	}

	// The process outlives this request: only the health wait honours ctx.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), exePath) // #nosec G204 -- path is a sha256-verified install from the catalogue
	cmd.Dir = filepath.Dir(exePath)
	cmd.Env = childEnv(env, port)
	cmd.Stdout, cmd.Stderr = logf, logf
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		_ = logf.Close()
		return nil, fmt.Errorf("%w: %w", ErrStartFailed, err)
	}
	p := &Proc{port: port, cmd: cmd, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		_ = logf.Close()
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		close(p.done)
	}()

	if err := r.waitHealthy(ctx, p); err != nil {
		r.terminate(p)
		return nil, fmt.Errorf("%w: %s: %w\n%s", ErrStartFailed, id, err, logTail(logPath))
	}
	if r.procs == nil {
		r.procs = map[string]*Proc{}
	}
	r.procs[id] = p
	return p, nil
}

// waitHealthy polls /healthz until 200, the process exits, the timeout
// passes or ctx ends.
func (r *Runner) waitHealthy(ctx context.Context, p *Proc) error {
	timeout := time.NewTimer(orDefault(r.HealthTimeout, DefaultHealthTimeout))
	defer timeout.Stop()
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	client := &http.Client{Timeout: time.Second}
	for {
		if healthy(ctx, client, p.URL()+"/healthz") {
			return nil
		}
		select {
		case <-p.done:
			return fmt.Errorf("exited before becoming healthy: %w", p.Err())
		case <-timeout.C:
			return errors.New("not healthy before timeout")
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func healthy(ctx context.Context, c *http.Client, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Stop terminates id's process (SIGTERM to its group, SIGKILL after
// StopTimeout) and forgets it. Stopping an unknown id is a no-op.
func (r *Runner) Stop(id string) error {
	r.mu.Lock()
	p, ok := r.procs[id]
	delete(r.procs, id)
	r.mu.Unlock()
	if ok {
		r.terminate(p)
	}
	return nil
}

// StopAll stops every process (daemon shutdown).
func (r *Runner) StopAll() {
	r.mu.Lock()
	procs := r.procs
	r.procs = nil
	r.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Go(func() { r.terminate(p) })
	}
	wg.Wait()
}

func (r *Runner) terminate(p *Proc) {
	signalGroup(p.cmd, false)
	grace := time.NewTimer(orDefault(r.StopTimeout, DefaultStopTimeout))
	defer grace.Stop()
	select {
	case <-p.done:
		return
	case <-grace.C:
	}
	signalGroup(p.cmd, true)
	<-p.done
}

// childEnv is deliberately minimal: the parent's environment (which may hold
// credentials) is not inherited.
func childEnv(env map[string]string, port int) []string {
	out := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return append(out, "PORT="+strconv.Itoa(port))
}

// freePort asks the kernel for an unused loopback port. Another process could
// take it before the server binds; the health check then fails cleanly.
func freePort(ctx context.Context) (int, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("pick port: %w", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func logTail(path string) string {
	f, err := os.Open(path) // #nosec G304 -- our own log file
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if fi, err := f.Stat(); err == nil && fi.Size() > logTailBytes {
		_, _ = f.Seek(-logTailBytes, io.SeekEnd)
	}
	b, _ := io.ReadAll(f)
	return "server log tail:\n" + strings.TrimSpace(string(b))
}

func safeID(id string) bool {
	return id != "" && !strings.HasPrefix(id, ".") && !strings.ContainsAny(id, `/\`) && filepath.IsLocal(id)
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// Done is closed once the process has exited.
func (p *Proc) Done() <-chan struct{} { return p.done }

// ErrClosed: the runner has been stopped (daemon shutdown); it wraps ErrStartFailed.
var ErrClosed = fmt.Errorf("%w: runner closed", ErrStartFailed)
