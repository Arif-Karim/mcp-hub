package runner_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/runner"
)

// fakeEnv switches the test binary into a fake MCP server (see TestMain).
const fakeEnv = "RUNNER_TEST_FAKE"

// TestMain lets the test binary double as the server under test: when
// RUNNER_TEST_FAKE is set it behaves as that mode and never runs the tests.
func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeEnv); mode != "" {
		fakeServer(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeServer(mode string) {
	fmt.Println("fake server starting:", mode, "home="+os.Getenv("HOME"), "secret="+os.Getenv("RUNNER_TEST_SECRET"), "extra="+os.Getenv("RUNNER_TEST_EXTRA"))
	fmt.Printf("%s pid=%d\n", mode, os.Getpid())
	switch mode {
	case "never-healthy":
		// time.Sleep, not select{}: a goroutine-free select{} trips Go's
		// deadlock detector and the "unhealthy" child would exit at once.
		time.Sleep(time.Hour)
	case "grandchild":
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Hour)
	case "with-grandchild":
		// Spawn a SIGTERM-ignoring grandchild in our process group, then serve.
		exe, _ := os.Executable()
		gc := exec.Command(exe) //nolint:gosec,noctx // test re-exec of itself
		gc.Env = append(os.Environ(), fakeEnv+"=grandchild")
		gc.Stdout, gc.Stderr = os.Stdout, os.Stderr
		if err := gc.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "grandchild:", err)
			os.Exit(4)
		}
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
	case "exit-now":
		fmt.Fprintln(os.Stderr, "boom: missing config")
		os.Exit(2)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/crash", func(http.ResponseWriter, *http.Request) {
		fmt.Fprintln(os.Stderr, "fake server crashing")
		os.Exit(3)
	})
	mux.HandleFunc("/cwd", func(w http.ResponseWriter, _ *http.Request) {
		wd, _ := os.Getwd()
		_, _ = w.Write([]byte(wd))
	})
	srv := &http.Server{Addr: "127.0.0.1:" + os.Getenv("PORT"), Handler: mux, ReadHeaderTimeout: time.Second}
	_ = srv.ListenAndServe()
}

func newRunner(t *testing.T) (*runner.Runner, string) {
	t.Helper()
	logs := filepath.Join(t.TempDir(), "logs")
	r := &runner.Runner{LogDir: logs, HealthTimeout: 5 * time.Second, StopTimeout: 2 * time.Second}
	t.Cleanup(r.StopAll)
	return r, logs
}

func self(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(url) //nolint:noctx // test helper against loopback
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// pidFromLog returns the pid a fake printed as "<mode> pid=N".
func pidFromLog(t *testing.T, path, mode string) int {
	t.Helper()
	var pid int
	waitUntil(t, mode+" pid in log", func() bool {
		data, _ := os.ReadFile(path) //nolint:gosec // test temp path
		m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(mode) + ` pid=(\d+)`).FindSubmatch(data)
		if m == nil {
			return false
		}
		pid, _ = strconv.Atoi(string(m[1]))
		return true
	})
	return pid
}

// gone reports whether pid no longer exists. An orphan is reaped by init
// shortly after it dies, so callers poll.
func gone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test temp path
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStartHealthy(t *testing.T) {
	r, logs := newRunner(t)
	t.Setenv("RUNNER_TEST_SECRET", "leaked") // must not reach the child
	p, err := r.Start(context.Background(), "svc", self(t), map[string]string{fakeEnv: "healthy", "RUNNER_TEST_EXTRA": "given"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !p.Running() || p.Err() != nil {
		t.Fatalf("Running = %v, Err = %v; want running, nil", p.Running(), p.Err())
	}
	if p.Port() <= 0 || p.URL() != fmt.Sprintf("http://127.0.0.1:%d", p.Port()) {
		t.Fatalf("URL = %q port = %d", p.URL(), p.Port())
	}
	if code, _ := get(t, p.URL()+"/healthz"); code != http.StatusOK {
		t.Fatalf("healthz = %d", code)
	}
	wantWD, _ := filepath.EvalSymlinks(filepath.Dir(self(t))) // macOS: /var -> /private/var
	if _, wd := get(t, p.URL()+"/cwd"); wd != wantWD {
		t.Fatalf("cwd = %q, want %q", wd, wantWD)
	}
	if got, ok := r.Get("svc"); !ok || got != p {
		t.Fatalf("Get = %v, %v", got, ok)
	}
	if _, ok := r.Get("other"); ok {
		t.Fatal("Get(other) found a process")
	}

	logPath := filepath.Join(logs, "svc.log")
	fi, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("log mode = %v, want 0600", fi.Mode().Perm())
	}
	if di, _ := os.Stat(logs); di.Mode().Perm() != 0o700 {
		t.Fatalf("log dir mode = %v, want 0700", di.Mode().Perm())
	}
	log := readLog(t, logPath)
	if !strings.Contains(log, "fake server starting: healthy") {
		t.Fatalf("log missing child stdout: %q", log)
	}
	if strings.Contains(log, "leaked") || !strings.Contains(log, "home="+os.Getenv("HOME")) || !strings.Contains(log, "extra=given") {
		t.Fatalf("child env wrong (want HOME + given vars, parent secret dropped): %q", log)
	}

	// Starting a running id again returns the same process.
	again, err := r.Start(context.Background(), "svc", self(t), map[string]string{fakeEnv: "healthy"})
	if err != nil || again != p {
		t.Fatalf("second Start = %v, %v; want the running process", again, err)
	}

	if err := r.Stop("svc"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if p.Running() {
		t.Fatal("still running after Stop")
	}
	if code, _ := get(t, p.URL()+"/healthz"); code != 0 {
		t.Fatalf("server still answering after Stop: %d", code)
	}
	if _, ok := r.Get("svc"); ok {
		t.Fatal("stopped process still tracked")
	}
	if err := r.Stop("svc"); err != nil {
		t.Fatalf("Stop of a stopped id: %v, want nil", err)
	}
}

func TestStartNeverHealthy(t *testing.T) {
	r, logs := newRunner(t)
	r.HealthTimeout = 400 * time.Millisecond
	start := time.Now()
	p, err := r.Start(context.Background(), "slow", self(t), map[string]string{fakeEnv: "never-healthy"})
	if !errors.Is(err, runner.ErrStartFailed) {
		t.Fatalf("err = %v, want ErrStartFailed", err)
	}
	if p != nil {
		t.Fatal("returned a process on failure")
	}
	if !strings.Contains(err.Error(), "fake server starting: never-healthy") {
		t.Fatalf("error should carry the log tail: %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v; health timeout not honoured", d)
	}
	if _, ok := r.Get("slow"); ok {
		t.Fatal("failed process still tracked")
	}
	// The live-but-unhealthy child was killed and reaped before Start returned.
	if pid := pidFromLog(t, filepath.Join(logs, "slow.log"), "never-healthy"); !gone(pid) {
		t.Fatalf("unhealthy child %d still exists after Start returned", pid)
	}
}

func TestStartExitsImmediately(t *testing.T) {
	r, _ := newRunner(t)
	start := time.Now()
	_, err := r.Start(context.Background(), "bad", self(t), map[string]string{fakeEnv: "exit-now"})
	if !errors.Is(err, runner.ErrStartFailed) || !strings.Contains(err.Error(), "boom: missing config") {
		t.Fatalf("err = %v, want ErrStartFailed with log tail", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v; should fail as soon as the process exits", d)
	}
}

func TestStartMissingBinary(t *testing.T) {
	r, _ := newRunner(t)
	_, err := r.Start(context.Background(), "gone", filepath.Join(t.TempDir(), "mcp-server"), nil)
	if !errors.Is(err, runner.ErrStartFailed) {
		t.Fatalf("err = %v, want ErrStartFailed", err)
	}
}

func TestStartRejectsUnsafeID(t *testing.T) {
	r, _ := newRunner(t)
	for _, id := range []string{"", "../x", "a/b", ".hidden"} {
		if _, err := r.Start(context.Background(), id, self(t), nil); !errors.Is(err, runner.ErrStartFailed) {
			t.Errorf("Start(%q) err = %v, want ErrStartFailed", id, err)
		}
	}
}

func TestStartCancelledContext(t *testing.T) {
	r, logs := newRunner(t)
	r.HealthTimeout = 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.Start(ctx, "slow", self(t), map[string]string{fakeEnv: "never-healthy"})
	if !errors.Is(err, runner.ErrStartFailed) || time.Since(start) > 3*time.Second {
		t.Fatalf("err = %v after %v; want prompt ErrStartFailed", err, time.Since(start))
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context error wrapped", err)
	}
	if pid := pidFromLog(t, filepath.Join(logs, "slow.log"), "never-healthy"); !gone(pid) {
		t.Fatalf("child %d still exists after cancelled Start", pid)
	}
}

func TestDefaultTimeouts(t *testing.T) {
	r := &runner.Runner{LogDir: filepath.Join(t.TempDir(), "logs")}
	t.Cleanup(r.StopAll)
	p, err := r.Start(context.Background(), "dflt", self(t), map[string]string{fakeEnv: "healthy"})
	if err != nil || !p.Running() {
		t.Fatalf("Start with zero timeouts: %v", err)
	}
}

func TestCrashAfterStart(t *testing.T) {
	r, logs := newRunner(t)
	p, err := r.Start(context.Background(), "crashy", self(t), map[string]string{fakeEnv: "healthy"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_, _ = get(t, p.URL()+"/crash")
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after /crash")
	}
	if p.Running() {
		t.Fatal("Running after Done closed")
	}
	if p.Err() == nil || !strings.Contains(p.Err().Error(), "exit status 3") {
		t.Fatalf("Err = %v, want exit status 3", p.Err())
	}
	if log := readLog(t, filepath.Join(logs, "crashy.log")); !strings.Contains(log, "fake server crashing") {
		t.Fatalf("stderr not logged: %q", log)
	}
	// A dead process can be replaced by a fresh Start.
	p2, err := r.Start(context.Background(), "crashy", self(t), map[string]string{fakeEnv: "healthy"})
	if err != nil || p2 == p || !p2.Running() {
		t.Fatalf("restart = %v, %v", p2, err)
	}
}

func TestStopEscalatesToKill(t *testing.T) {
	r, _ := newRunner(t)
	r.StopTimeout = 300 * time.Millisecond
	p, err := r.Start(context.Background(), "stubborn", self(t), map[string]string{fakeEnv: "ignore-term"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	start := time.Now()
	if err := r.Stop("stubborn"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if p.Running() {
		t.Fatal("still running after Stop")
	}
	if d := time.Since(start); d < 250*time.Millisecond || d > 3*time.Second {
		t.Fatalf("Stop took %v; want SIGTERM grace then SIGKILL", d)
	}
}

func TestStopAll(t *testing.T) {
	r, _ := newRunner(t)
	a, err := r.Start(context.Background(), "a1", self(t), map[string]string{fakeEnv: "healthy"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Start(context.Background(), "b1", self(t), map[string]string{fakeEnv: "healthy"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Port() == b.Port() {
		t.Fatal("two servers share a port")
	}
	r.StopAll()
	if a.Running() || b.Running() {
		t.Fatal("StopAll left a process running")
	}
	if _, ok := r.Get("a1"); ok {
		t.Fatal("StopAll left a process tracked")
	}
}

func TestStopKillsWholeProcessGroup(t *testing.T) {
	r, logs := newRunner(t)
	r.StopTimeout = 5 * time.Second
	p, err := r.Start(context.Background(), "family", self(t), map[string]string{fakeEnv: "with-grandchild"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	gc := pidFromLog(t, filepath.Join(logs, "family.log"), "grandchild")
	if gone(gc) {
		t.Fatal("grandchild not running before Stop")
	}
	start := time.Now()
	if err := r.Stop("family"); err != nil {
		t.Fatal(err)
	}
	// The leader exits on SIGTERM at once; the SIGTERM-ignoring grandchild must
	// not be orphaned, and Stop must not wait out the whole grace period for it.
	waitUntil(t, "grandchild killed", func() bool { return gone(gc) })
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Stop + reap took %v", d)
	}
	if code, _ := get(t, p.URL()+"/healthz"); code != 0 {
		t.Fatal("port still served after Stop")
	}
}

func TestCrashedLeaderTakesGroupWithIt(t *testing.T) {
	r, logs := newRunner(t)
	p, err := r.Start(context.Background(), "family", self(t), map[string]string{fakeEnv: "with-grandchild"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	gc := pidFromLog(t, filepath.Join(logs, "family.log"), "grandchild")
	_, _ = get(t, p.URL()+"/crash")
	<-p.Done()
	waitUntil(t, "grandchild of a crashed server killed", func() bool { return gone(gc) })
}

func TestStartDoesNotBlockOtherIDs(t *testing.T) {
	r, _ := newRunner(t)
	r.HealthTimeout = 3 * time.Second
	slow := make(chan error, 1)
	go func() {
		_, err := r.Start(context.Background(), "slow", self(t), map[string]string{fakeEnv: "never-healthy"})
		slow <- err
	}()
	time.Sleep(200 * time.Millisecond) // let the slow start reach its health wait
	begin := time.Now()
	if _, ok := r.Get("slow"); ok {
		t.Fatal("unhealthy process visible before it is healthy")
	}
	p, err := r.Start(context.Background(), "fast", self(t), map[string]string{fakeEnv: "healthy"})
	if err != nil || !p.Running() {
		t.Fatalf("fast start: %v", err)
	}
	if d := time.Since(begin); d > 2*time.Second {
		t.Fatalf("Get/Start of another id waited %v behind a slow health check", d)
	}
	if err := <-slow; !errors.Is(err, runner.ErrStartFailed) {
		t.Fatalf("slow start err = %v", err)
	}
}

func TestStopAllWaitsForInFlightStartAndRefusesNew(t *testing.T) {
	r, logs := newRunner(t)
	r.HealthTimeout = 2 * time.Second
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		_, err := r.Start(context.Background(), "late", self(t), map[string]string{fakeEnv: "never-healthy"})
		result <- err
	}()
	<-started
	pid := pidFromLog(t, filepath.Join(logs, "late.log"), "never-healthy")
	r.StopAll()
	// StopAll returned: the in-flight start's process must already be gone.
	if !gone(pid) {
		waitUntil(t, "in-flight child reaped", func() bool { return gone(pid) })
	}
	select {
	case err := <-result:
		if !errors.Is(err, runner.ErrStartFailed) {
			t.Fatalf("in-flight start err = %v", err)
		}
	default:
		t.Fatal("StopAll returned before the in-flight Start finished")
	}
	if _, err := r.Start(context.Background(), "after", self(t), map[string]string{fakeEnv: "healthy"}); !errors.Is(err, runner.ErrClosed) {
		t.Fatalf("Start after StopAll = %v, want ErrClosed", err)
	}
}
