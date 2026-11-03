package catalogue

import (
	"fmt"
	"slices"
)

// DefaultCatalogue parses a fresh copy of the catalogue embedded in the binary.
func DefaultCatalogue() (*Catalogue, error) {
	c, err := Parse(defaultYAML)
	if err != nil {
		return nil, fmt.Errorf("embedded catalogue: %w", err)
	}
	return c, nil
}

// Merge returns a copy of base with extra applied in order: an entry whose ID
// already exists replaces it in place, otherwise it is appended. base is not modified.
func Merge(base *Catalogue, extra ...Entry) *Catalogue {
	out := &Catalogue{Version: base.Version, Entries: slices.Clone(base.Entries)}
	for _, e := range extra {
		if i := slices.IndexFunc(out.Entries, func(x Entry) bool { return x.ID == e.ID }); i >= 0 {
			out.Entries[i] = e
		} else {
			out.Entries = append(out.Entries, e)
		}
	}
	return out
}
