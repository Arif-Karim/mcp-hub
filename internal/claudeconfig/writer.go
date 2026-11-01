package claudeconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

// mu serialises every read-modify-write in this process (all Writers share the files).
var mu sync.Mutex

// errChanged means the config changed between our read and our rename.
var errChanged = errors.New("config changed during write")

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
		if cur, ok := servers[name]; ok {
			if err := checkOwned(id, cur, owned); err != nil {
				return false, err
			}
			if owned[id].Hash == hash {
				return false, nil // already exactly what we want
			}
		}
		servers[name] = raw
		owned[id] = Owned{ID: id, ConfigName: name, URL: e.URL, Hash: hash, EnabledAt: w.now()}
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
// write. Entries the user edited are left in place and reported together as
// an ErrConfigConflict after the others have been removed.
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
		return fmt.Errorf("%w: left user-modified entries in place: %s", ErrConfigConflict, strings.Join(skipped, ", "))
	}
	return nil
}

// Owned returns the ledger records keyed by id (empty, never nil, without a ledger).
func (w *Writer) Owned() (map[string]Owned, error) {
	mu.Lock()
	defer mu.Unlock()
	return w.readLedger()
}

// checkOwned reports ErrConfigConflict unless cur is an entry the ledger says
// we wrote and whose content still matches the recorded hash.
func checkOwned(id string, cur json.RawMessage, owned map[string]Owned) error {
	rec, ok := owned[id]
	if !ok {
		return fmt.Errorf("%w: %s exists but was not created by mcp-hub", ErrConfigConflict, ConfigName(id))
	}
	if hashEntry(cur) != rec.Hash {
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

// editFunc mutates the mcpServers map and ledger in place and reports whether
// the config file needs rewriting. Returning an error aborts without writing.
type editFunc func(servers map[string]json.RawMessage, owned map[string]Owned) (configChanged bool, err error)

// update runs one locked read-modify-write cycle. If the config changes between
// our read and our rename (Claude Code writes it often), the edit is re-applied
// once to the fresh content; a second change is reported as ErrConfigConflict.
//
// Ledger ordering: before touching the config we persist old ∪ new records, so
// a crash can only leave the ledger claiming an entry that is absent (harmless:
// Add re-creates it, Remove drops the record) and never leave a hub entry the
// ledger does not know about (which would lock it as "user-owned").
func (w *Writer) update(edit editFunc) error {
	mu.Lock()
	defer mu.Unlock()
	target, err := resolveTarget(w.ClaudeConfig)
	if err != nil {
		return err
	}
	before, err := w.readLedger()
	if err != nil {
		return err
	}
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
			if maps.Equal(before, owned) {
				return nil
			}
			return w.writeLedger(owned)
		}
		union := maps.Clone(before)
		maps.Copy(union, owned)
		if err := w.writeLedger(union); err != nil {
			return err
		}
		err = w.writeConfig(target, snap)
		if errors.Is(err, errChanged) && attempt == 1 {
			continue
		}
		if err != nil {
			_ = w.writeLedger(before) // best effort: the config was not changed
			if errors.Is(err, errChanged) {
				return fmt.Errorf("%w: config changed during write, retry", ErrConfigConflict)
			}
			return err
		}
		return w.writeLedger(owned)
	}
}
