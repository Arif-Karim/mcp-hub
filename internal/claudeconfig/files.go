package claudeconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	maxBackups   = 5
	backupPrefix = "claude.json."
	backupLayout = "20060102T150405.000000000Z"
	defaultMode  = fs.FileMode(0o600)
	ledgerFormat = 1
)

// snapshot is the config as read: exact bytes (for the lost-update check), its
// mode, and the decoded top level / mcpServers.
type snapshot struct {
	exists  bool
	raw     []byte
	mode    fs.FileMode
	info    fs.FileInfo // nil for a new file
	top     map[string]json.RawMessage
	servers map[string]json.RawMessage
}

// resolveTarget follows symlinks so we replace the real file and keep the link
// (people symlink dotfiles). A missing file is fine; a dangling link is not.
func resolveTarget(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if _, lerr := os.Lstat(path); errors.Is(lerr, fs.ErrNotExist) {
		return path, nil
	}
	return "", fmt.Errorf("resolve claude config %s: %w", path, err)
}

// readConfig decodes both levels into json.RawMessage so every value we do not
// own is carried through untouched. Key order is not preserved (Go maps), which
// is semantically irrelevant to JSON consumers.
func readConfig(path string) (snapshot, error) {
	s := snapshot{mode: defaultMode, top: map[string]json.RawMessage{}, servers: map[string]json.RawMessage{}}
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the configured Claude config
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return s, err
	}
	s.exists, s.raw, s.mode, s.info = true, raw, fi.Mode().Perm(), fi
	s.top = nil
	if err := json.Unmarshal(raw, &s.top); err != nil || s.top == nil {
		return s, fmt.Errorf("%w: %s is not a JSON object", ErrConfigInvalid, path)
	}
	if v, ok := s.top["mcpServers"]; ok {
		s.servers = nil
		if err := json.Unmarshal(v, &s.servers); err != nil || s.servers == nil {
			return s, fmt.Errorf("%w: mcpServers in %s is not an object", ErrConfigInvalid, path)
		}
	}
	return s, nil
}

// encode writes v as 2-space-indented JSON (like Claude Code), without HTML
// escaping so untouched strings keep their bytes.
func encode(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// writeConfig backs up the current file, then atomically replaces it with
// snap.top + snap.servers, failing with errChanged if the file moved under us.
func (w *Writer) writeConfig(target string, snap snapshot) error {
	servers, err := encode(snap.servers)
	if err != nil {
		return err
	}
	snap.top["mcpServers"] = servers
	data, err := encode(snap.top)
	if err != nil {
		return err
	}
	if snap.exists {
		if err := w.backup(snap.raw); err != nil {
			return err
		}
	}
	return writeAtomic(target, data, snap.mode, snap.info, func() error {
		if w.beforeRename != nil {
			w.beforeRename()
		}
		cur, err := os.ReadFile(target) // #nosec G304 -- path is the configured Claude config
		exists := err == nil
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if exists != snap.exists || !bytes.Equal(cur, snap.raw) {
			return errChanged
		}
		return nil
	})
}

// backupName matches only files backup created, so pruning never deletes anything else.
var backupName = regexp.MustCompile(`^claude\.json\.\d{8}T\d{6}\.\d{9}Z(-\d{3})?$`)

// backup stores the pre-write bytes (fsync'd, never overwriting an existing
// backup even if the clock repeats) and keeps only the newest maxBackups.
func (w *Writer) backup(raw []byte) error {
	if err := os.MkdirAll(w.BackupDir, 0o700); err != nil {
		return err
	}
	ours, err := w.listBackups()
	if err != nil {
		return err
	}
	name := backupPrefix + w.now().UTC().Format(backupLayout)
	if n := len(ours); n > 0 && name <= ours[n-1] {
		// Same (or earlier) timestamp as the newest backup: sort after it instead.
		if name, err = nextBackupName(ours[n-1]); err != nil {
			return err
		}
	}
	// O_EXCL: never overwrite an existing backup.
	f, err := os.OpenFile(filepath.Join(w.BackupDir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- under BackupDir
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	ours = append(ours, name)
	for len(ours) > maxBackups {
		if err := os.Remove(filepath.Join(w.BackupDir, ours[0])); err != nil {
			return err
		}
		ours = ours[1:]
	}
	return nil
}

// listBackups returns our backup names, oldest first (timestamps and
// zero-padded suffixes sort lexically).
func (w *Writer) listBackups() ([]string, error) {
	ents, err := os.ReadDir(w.BackupDir)
	if err != nil {
		return nil, err
	}
	var ours []string
	for _, e := range ents {
		if backupName.MatchString(e.Name()) {
			ours = append(ours, e.Name())
		}
	}
	slices.Sort(ours)
	return ours, nil
}

// nextBackupName returns the name that sorts immediately after last.
func nextBackupName(last string) (string, error) {
	stamp, suffix, _ := strings.Cut(last, "Z-")
	n := 0
	if suffix != "" {
		if _, err := fmt.Sscanf(suffix, "%03d", &n); err != nil {
			return "", err
		}
	}
	if n >= 999 {
		return "", errors.New("too many backups with the same timestamp")
	}
	return fmt.Sprintf("%sZ-%03d", strings.TrimSuffix(stamp, "Z"), n+1), nil
}

// writeAtomic writes data to a temp file in path's directory (same filesystem),
// gives it orig's (best effort) owner and the given mode, fsyncs it, runs
// check, then renames it over path.
func writeAtomic(path string, data []byte, mode fs.FileMode, orig fs.FileInfo, check func() error) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if orig != nil {
		chownLike(f, orig)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err == nil {
		if err = f.Chmod(mode); err == nil {
			err = f.Sync()
		}
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if check != nil {
		if err = check(); err != nil {
			return err
		}
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	if d, derr := os.Open(dir); derr == nil { // #nosec G304 -- directory of the target
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

type ledgerFile struct {
	Version int              `json:"version"`
	Servers map[string]Owned `json:"servers"`
}

func (w *Writer) readLedger() (map[string]Owned, error) {
	raw, err := os.ReadFile(w.Ledger)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Owned{}, nil
	}
	if err != nil {
		return nil, err
	}
	var l ledgerFile
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("read ledger %s: %w", w.Ledger, err)
	}
	if l.Version != ledgerFormat {
		return nil, fmt.Errorf("read ledger %s: unsupported version %d", w.Ledger, l.Version)
	}
	if l.Servers == nil {
		l.Servers = map[string]Owned{}
	}
	return l.Servers, nil
}

func (w *Writer) writeLedger(owned map[string]Owned) error {
	if err := os.MkdirAll(filepath.Dir(w.Ledger), 0o700); err != nil {
		return err
	}
	data, err := encode(ledgerFile{Version: ledgerFormat, Servers: owned})
	if err != nil {
		return err
	}
	return writeAtomic(w.Ledger, data, 0o600, nil, nil)
}
