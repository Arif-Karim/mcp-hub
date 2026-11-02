package directory

import "testing"

func TestEmbeddedDataIsComplete(t *testing.T) {
	services := load()
	if len(services) != 8 {
		t.Fatalf("got %d services, want 8", len(services))
	}
	seen := map[string]bool{}
	for _, s := range services {
		if seen[s.Name] {
			t.Errorf("duplicate service %q", s.Name)
		}
		seen[s.Name] = true
		if s.Team == "" || s.Channel == "" || s.Runbook == "" || len(s.Tags) == 0 || s.Tier < 1 || s.Tier > 3 {
			t.Errorf("incomplete service: %+v", s.Name)
		}
	}
}
