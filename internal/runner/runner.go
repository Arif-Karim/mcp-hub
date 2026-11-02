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

// ErrClosed: the runner has been stopped (daemon shutdown). It wraps ErrStartFailed.
var ErrClosed = fmt.Errorf("%w: runner closed", ErrStartFailed)

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

	mu       sync.Mutex // guards the fields below; never held while waiting
	procs    map[string]*Proc
	locks    map[string]*sync.Mutex // per-id: serialises Start/Stop of one id
	closed   bool
	quit     chan struct{} // closed by StopAll; aborts in-flight health waits
	inflight sync.WaitGroup
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

// Done is closed once the process has exited.
func (p *Proc) Done() <-chan struct{} { return p.done }

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
	quit, err := r.enter()
	if err != nil {
		return nil, err
	}
	defer r.inflight.Done()
	defer r.lockID(id)()
	if p, ok := r.Get(id); ok && p.Running() {
		return p, nil
	}
	r.forget(id)

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
		// Whatever ended the leader, take the rest of its group with it so no
		// grandchild is orphaned (it would keep the port or the log open).
		signalGroup(cmd, true)
		_ = logf.Close()
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		close(p.done)
	}()

	if err := r.waitHealthy(ctx, quit, p); err != nil {
		r.terminate(p)
		return nil, fmt.Errorf("%w: %s: %w\n%s", ErrStartFailed, id, err, logTail(logPath))
	}
	r.mu.Lock()
	if r.closed { // StopAll ran while we were starting
		r.mu.Unlock()
		r.terminate(p)
		return nil, ErrClosed
	}
	if r.procs == nil {
		r.procs = map[string]*Proc{}
	}
	r.procs[id] = p
	r.mu.Unlock()
	return p, nil
}

// enter registers an in-flight Start, or fails once StopAll has run.
func (r *Runner) enter() (<-chan struct{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrClosed
	}
	if r.quit == nil {
		r.quit = make(chan struct{})
	}
	r.inflight.Add(1)
	return r.quit, nil
}

func (r *Runner) forget(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.procs, id)
}

// lockID serialises Start/Stop of one id and returns the unlock func.
func (r *Runner) lockID(id string) func() {
	r.mu.Lock()
	if r.locks == nil {
		r.locks = map[string]*sync.Mutex{}
	}
	l, ok := r.locks[id]
	if !ok {
		l = &sync.Mutex{}
		r.locks[id] = l
	}
	r.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// waitHealthy polls /healthz until 200, the process exits, the timeout
// passes, ctx ends or StopAll runs.
func (r *Runner) waitHealthy(ctx context.Context, quit <-chan struct{}, p *Proc) error {
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
		case <-quit:
			return ErrClosed
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

// Stop terminates id's process (SIGTERM to its group, SIGKILL to the group
// after StopTimeout) and forgets it. A Start of the same id in progress is
// waited for first. Stopping an unknown id is a no-op.
func (r *Runner) Stop(id string) error {
	defer r.lockID(id)()
	r.mu.Lock()
	p, ok := r.procs[id]
	delete(r.procs, id)
	r.mu.Unlock()
	if ok {
		r.terminate(p)
	}
	return nil
}

// StopAll is daemon shutdown: it refuses new Starts, aborts in-flight ones
// (their processes are killed), waits for them, then stops every process.
func (r *Runner) StopAll() {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		if r.quit != nil {
			close(r.quit)
		}
	}
	r.mu.Unlock()
	r.inflight.Wait()

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

// terminate sends SIGTERM to the group, escalates to SIGKILL after the grace
// period, and returns once the leader is reaped (the reaper then SIGKILLs any
// group members left, e.g. a grandchild that ignores SIGTERM).
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
