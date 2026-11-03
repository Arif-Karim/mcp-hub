package catalogue

import (
	"slices"
	"testing"
)

func TestDefaultCatalogueParses(t *testing.T) {
	c, err := DefaultCatalogue()
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != 1 || len(c.Entries) == 0 || c.Entries[0].ID != "service-directory" {
		t.Fatalf("unexpected default catalogue: version %d, %d entries", c.Version, len(c.Entries))
	}
	// Each call returns a fresh copy, so callers may mutate it.
	c.Entries[0].ID = "mutated"
	again, _ := DefaultCatalogue()
	if again.Entries[0].ID != "service-directory" {
		t.Fatal("DefaultCatalogue shares state between calls")
	}
}

func TestMergeReplacesByIDAndAppendsNew(t *testing.T) {
	base := &Catalogue{Version: 1, Entries: []Entry{{ID: "aa", Name: "A"}, {ID: "bb", Name: "B"}}}
	got := Merge(base, Entry{ID: "bb", Name: "B2"}, Entry{ID: "cc", Name: "C"}, Entry{ID: "cc", Name: "C2"})
	ids := make([]string, 0, len(got.Entries))
	names := make([]string, 0, len(got.Entries))
	for _, e := range got.Entries {
		ids = append(ids, e.ID)
		names = append(names, e.Name)
	}
	if !slices.Equal(ids, []string{"aa", "bb", "cc"}) || !slices.Equal(names, []string{"A", "B2", "C2"}) {
		t.Fatalf("ids %v names %v", ids, names)
	}
	if got.Version != 1 {
		t.Fatalf("version %d", got.Version)
	}
	if base.Entries[1].Name != "B" || len(base.Entries) != 2 {
		t.Fatal("Merge mutated its input")
	}
}
