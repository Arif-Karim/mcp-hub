package claudeconfig

import "errors"

var errNotImplemented = errors.New("not implemented")

// Add writes hub__<id> pointing at e and records it in the ledger.
func (w *Writer) Add(id string, e ServerEntry) error { return errNotImplemented }

// Remove deletes hub__<id> if the hub owns it and it is unmodified.
func (w *Writer) Remove(id string) error { return errNotImplemented }

// RemoveAll removes every ledger-owned, unmodified entry.
func (w *Writer) RemoveAll() error { return errNotImplemented }

// Owned returns the ledger records keyed by id.
func (w *Writer) Owned() (map[string]Owned, error) { return nil, errNotImplemented }

const maxBackups = 5
