package claudeconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sync"
)

// mu serialises read-modify-write cycles within this process; the flock on
// lockName serialises them across processes (daemon + CLI).
var mu sync.Mutex

const lockName = "claudeconfig.lock"

// errChanged means the config changed between our read and our rename.
var errChanged = errors.New("config changed during write")

// lock takes the in-process mutex and the cross-process flock.
func (w *Writer) lock() (func(), error) {
	mu.Lock()
	unlock, err := lockFile(filepath.Join(filepath.Dir(w.Ledger), lockName))
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	return func() { unlock(); mu.Unlock() }, nil
}

// Add writes hub__<id> pointing at e and records it in the ledger. It refuses
// (ErrConfigConflict) to overwrite an entry the hub did not create or that the
// user has edited since; re-adding an unchanged owned entry is idempotent.
func (w *Writer) Add(id string, e ServerEntry) error {
	if err := validateID(id); err != nil {
		return err
	}
	name := ConfigName(id)
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	hash := hashEntry(raw)
	return w.update(func(servers map[string]json.RawMessage, owned map[string]Owned) (bool, error) {
		prev := ""
		if cur, ok := servers[name]; ok {
			if err := checkOwned(id, cur, owned); err != nil {
				return false, err
			}
			prev = hashEntry(cur)
			if prev == hash { // config already right; at most the ledger needs settling
				rec := owned[id]
				rec.URL, rec.Hash, rec.PrevHash = e.URL, hash, ""
				owned[id] = rec
				return false, nil
			}
		}
		servers[name] = raw
		owned[id] = Owned{ID: id, ConfigName: name, URL: e.URL, Hash: hash, PrevHash: prev, EnabledAt: w.now()}
		return true, nil
	})
}

// Remove deletes hub__<id> if the hub owns it and it is unmodified. Removing an
// id that is neither in the config nor the ledger is a no-op; an owned entry
// that is already gone just has its ledger record dropped.
func (w *Writer) Remove(id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	name := ConfigName(id)
	return w.update(func(servers map[string]json.RawMessage, owned map[string]Owned) (bool, error) {
		cur, present := servers[name]
		if !present {
			delete(owned, id)
			return false, nil
		}
		if err := checkOwned(id, cur, owned); err != nil {
			return false, err
		}
		delete(servers, name)
		delete(owned, id)
		return true, nil
	})
}

// RemoveAll (uninstall) removes every ledger-owned, unmodified entry in one
// write. Entries the user edited are left in place and reported as a
// *PartialRemoveError (which is also an ErrConfigConflict).
func (w *Writer) RemoveAll() error {
	var skipped []string
	err := w.update(func(servers map[string]json.RawMessage, owned map[string]Owned) (bool, error) {
		skipped = nil // update may re-apply after a concurrent change
		changed := false
		for _, id := range slices.Sorted(maps.Keys(owned)) {
			cur, present := servers[ConfigName(id)]
			if !present {
				delete(owned, id)
				continue
			}
			if checkOwned(id, cur, owned) != nil {
				skipped = append(skipped, ConfigName(id))
				continue
			}
			delete(servers, ConfigName(id))
			delete(owned, id)
			changed = true
		}
		return changed, nil
	})
	if err != nil {
		return err
	}
	if len(skipped) > 0 {
		return &PartialRemoveError{Skipped: skipped}
	}
	return nil
}

// Owned returns the ledger records keyed by id (empty, never nil, without a ledger).
func (w *Writer) Owned() (map[string]Owned, error) {
	unlock, err := w.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	return w.readLedger()
}

// checkOwned reports ErrConfigConflict unless cur is an entry the ledger says
// we wrote and whose content matches the recorded hash (or the previous hash of
// an interrupted change).
func checkOwned(id string, cur json.RawMessage, owned map[string]Owned) error {
	rec, ok := owned[id]
	if !ok {
		return fmt.Errorf("%w: %s exists but was not created by mcp-hub", ErrConfigConflict, ConfigName(id))
	}
	if h := hashEntry(cur); h != rec.Hash && (rec.PrevHash == "" || h != rec.PrevHash) {
		return fmt.Errorf("%w: %s was modified outside mcp-hub", ErrConfigConflict, ConfigName(id))
	}
	return nil
}

// hashEntry is the sha256 of the entry's canonical JSON (sorted keys, compact),
// so Claude Code re-serialising the file with different whitespace or key order
// is not mistaken for a user edit.
func hashEntry(raw json.RawMessage) string {
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	canon := []byte(raw)
	if d.Decode(&v) == nil {
		if b, err := json.Marshal(v); err == nil {
			canon = b
		}
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:])
}

// settle clears PrevHash on records whose config entry now matches Hash, i.e.
// whose change has landed. Records still pending (a crashed change) keep it.
func settle(owned map[string]Owned, servers map[string]json.RawMessage) map[string]Owned {
	out := maps.Clone(owned)
	for id, rec := range out {
		if cur, ok := servers[rec.ConfigName]; ok && hashEntry(cur) == rec.Hash {
			rec.PrevHash = ""
			out[id] = rec
		}
	}
	return out
}

// editFunc mutates the mcpServers map and ledger in place and reports whether
// the config file needs rewriting. Returning an error aborts without writing.
type editFunc func(servers map[string]json.RawMessage, owned map[string]Owned) (configChanged bool, err error)

// update runs one locked read-modify-write cycle. If the config changes between
// our read and our rename (Claude Code writes it often), the edit is re-applied
// once to the fresh content; a second change is reported as ErrConfigConflict.
//
// Crash ordering — ledger first, config second, ledger again:
//  1. ledger := before ∪ new records (new records carry PrevHash = the entry they replace)
//  2. config renamed into place
//  3. ledger := new records, settled (PrevHash cleared)
//
// A crash after 1 leaves the ledger claiming an entry that is absent or still
// at PrevHash — both are recognised as ours, so the next call self-heals. The
// opposite order could leave a hub__ entry the ledger does not know, which
// would be locked forever as "user-owned". Any error after step 1 and before
// step 2 completes restores the previous ledger, so a failed call never leaves
// the ledger claiming an entry it did not write.
func (w *Writer) update(edit editFunc) (err error) {
	unlock, err := w.lock()
	if err != nil {
		return err
	}
	defer unlock()
	target, err := resolveTarget(w.ClaudeConfig)
	if err != nil {
		return err
	}
	before, err := w.readLedger()
	if err != nil {
		return err
	}
	inflight := false
	defer func() {
		if err != nil && inflight {
			_ = w.writeLedger(before) // best effort; the config was not changed
		}
	}()
	for attempt := 1; ; attempt++ {
		snap, err := readConfig(target)
		if err != nil {
			return err
		}
		owned := maps.Clone(before)
		changed, err := edit(snap.servers, owned)
		if err != nil {
			return err
		}
		if !changed {
			owned = settle(owned, snap.servers)
			if maps.Equal(before, owned) {
				return nil
			}
			return w.writeLedger(owned)
		}
		union := maps.Clone(before)
		maps.Copy(union, owned)
		inflight = true
		if err := w.writeLedger(union); err != nil {
			return err
		}
		err = w.writeConfig(target, snap)
		if errors.Is(err, errChanged) && attempt == 1 {
			continue
		}
		if errors.Is(err, errChanged) {
			return fmt.Errorf("%w: config changed during write, retry", ErrConfigConflict)
		}
		if err != nil {
			return err
		}
		inflight = false // config landed: the union ledger is now the safe fallback
		return w.writeLedger(settle(owned, snap.servers))
	}
}
