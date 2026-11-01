package catalogue

import (
	_ "embed" // default catalogue
	"fmt"
	"os"
	"slices"
	"strings"
)

//go:embed default.yaml
var defaultYAML []byte

// Store is a read-only catalogue with lookup and search.
type Store struct {
	entries []Entry
	byID    map[string]int
}

// NewStore indexes c, which should already be validated.
func NewStore(c *Catalogue) *Store {
	s := &Store{entries: c.Entries, byID: make(map[string]int, len(c.Entries))}
	for i, e := range c.Entries {
		s.byID[e.ID] = i
	}
	return s
}

// Default returns the catalogue embedded in the binary.
func Default() (*Store, error) {
	c, err := Parse(defaultYAML)
	if err != nil {
		return nil, fmt.Errorf("embedded catalogue: %w", err)
	}
	return NewStore(c), nil
}

// Load reads, parses and validates a catalogue file.
func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- operator-supplied catalogue path
	if err != nil {
		return nil, fmt.Errorf("read catalogue: %w", err)
	}
	c, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return NewStore(c), nil
}

// Get returns the entry with the given id, or ErrNotFound.
func (s *Store) Get(id string) (Entry, error) {
	i, ok := s.byID[id]
	if !ok {
		return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return s.entries[i], nil
}

// Search returns entries, in file order, whose id, name, description, tags or
// tool names contain q (case-insensitive). kind "" matches every kind.
func (s *Store) Search(q string, kind Kind) []Entry {
	q = strings.ToLower(q)
	out := []Entry{}
	for _, e := range s.entries {
		if kind != "" && e.Kind != kind {
			continue
		}
		if matches(e, q) {
			out = append(out, e)
		}
	}
	return out
}

func matches(e Entry, q string) bool {
	fields := append([]string{e.ID, e.Name, e.Description}, e.Tags...)
	for _, t := range e.Tools {
		fields = append(fields, t.Name)
	}
	return slices.ContainsFunc(fields, func(f string) bool {
		return strings.Contains(strings.ToLower(f), q)
	})
}
