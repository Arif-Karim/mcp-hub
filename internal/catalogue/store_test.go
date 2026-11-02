package catalogue_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
)

func ids(es []catalogue.Entry) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}

func testStore() *catalogue.Store {
	a := validLocal()
	a.Tags = []string{"Platform"}
	a.Tools = []catalogue.Tool{{Name: "find_service", Description: "x"}}
	b := validRemote()
	b.Name = "DeepWiki"
	b.Description = "Ask about REPOS"
	c := validRemote()
	c.ID = "zeta"
	c.Description = "other"
	return catalogue.NewStore(&catalogue.Catalogue{Version: 1, Entries: []catalogue.Entry{a, b, c}})
}

func TestSearch(t *testing.T) {
	s := testStore()
	cases := []struct {
		q    string
		kind catalogue.Kind
		want []string
	}{
		{"", "", []string{"svc-dir", "remote-one", "zeta"}},
		{"", catalogue.KindRemote, []string{"remote-one", "zeta"}},
		{"", catalogue.KindLocal, []string{"svc-dir"}},
		{"SVC", "", []string{"svc-dir"}},         // id
		{"deepwiki", "", []string{"remote-one"}}, // name
		{"repos", "", []string{"remote-one"}},    // description
		{"platform", "", []string{"svc-dir"}},    // tag
		{"FIND_SERV", "", []string{"svc-dir"}},   // tool name
		{"zeta", catalogue.KindLocal, []string{}},
		{"nothing-matches", "", []string{}},
	}
	for _, tc := range cases {
		got := ids(s.Search(tc.q, tc.kind))
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Search(%q,%q) = %v, want %v", tc.q, tc.kind, got, tc.want)
		}
	}
}

func TestGet(t *testing.T) {
	s := testStore()
	e, err := s.Get("zeta")
	if err != nil || e.ID != "zeta" || e.Description != "other" {
		t.Fatalf("Get = %+v, %v", e, err)
	}
	if _, err := s.Get("missing"); !errors.Is(err, catalogue.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDefaultCatalogueLoads(t *testing.T) {
	s, err := catalogue.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	want := []string{"service-directory", "deepwiki", "context7", "jira-rovo", "payments-sandbox"}
	if got := ids(s.Search("", "")); !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	dw, err := s.Get("deepwiki")
	if err != nil {
		t.Fatal(err)
	}
	if dw.Name != "Engineering Docs Q&A (DeepWiki)" {
		t.Errorf("deepwiki name = %q", dw.Name)
	}
	const desc = "Ask questions about repositories' documentation. In production this would be a company-hosted " +
		"DeepWiki over internal repos; this demo points at the public instance (no credentials sent)."
	if dw.Description != desc {
		t.Errorf("deepwiki description = %q", dw.Description)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(good, []byte("version: 1\nentries: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err := catalogue.Load(good); err != nil || len(s.Search("", "")) != 0 {
		t.Fatalf("Load good: %v", err)
	}
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("version: 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := catalogue.Load(bad); !errors.Is(err, catalogue.ErrInvalid) {
		t.Fatalf("Load bad: err = %v, want ErrInvalid", err)
	}
	if _, err := catalogue.Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Fatal("Load missing: want error")
	}
}

func TestCanEnable(t *testing.T) {
	for a, ok := range map[catalogue.Approval]bool{
		catalogue.ApprovalApproved: true,
		catalogue.ApprovalPending:  false,
		catalogue.ApprovalRevoked:  false,
	} {
		err := catalogue.CanEnable(catalogue.Entry{ID: "x", Approval: a})
		if ok && err != nil {
			t.Errorf("%s: err = %v, want nil", a, err)
		}
		if !ok && !errors.Is(err, catalogue.ErrNotApproved) {
			t.Errorf("%s: err = %v, want ErrNotApproved", a, err)
		}
	}
}

func TestConfigName(t *testing.T) {
	if got := catalogue.ConfigName("deepwiki"); got != "hub__deepwiki" {
		t.Fatalf("ConfigName = %q", got)
	}
}

func TestInstallStateJSON(t *testing.T) {
	b, _ := json.Marshal(catalogue.InstallState{Status: catalogue.StatusDisabled, ConfigName: "hub__x"})
	if got, want := string(b), `{"enabled":false,"status":"disabled","configName":"hub__x"}`; got != want {
		t.Fatalf("json = %s, want %s", got, want)
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	b, _ = json.Marshal(catalogue.InstallState{
		Enabled: true, Status: catalogue.StatusRunning, ConfigName: "hub__x",
		ProxyURL: "http://127.0.0.1:7420/mcp/x", EnabledAt: at, LastError: "boom",
	})
	want := `{"enabled":true,"status":"running","configName":"hub__x","proxyUrl":"http://127.0.0.1:7420/mcp/x","enabledAt":"2026-01-02T03:04:05Z","lastError":"boom"}`
	if string(b) != want {
		t.Fatalf("json = %s, want %s", b, want)
	}
}

func TestArtifactsNeverSerialised(t *testing.T) {
	b, _ := json.Marshal(validLocal())
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["artifacts"]; ok {
		t.Fatalf("artifacts leaked into JSON: %s", b)
	}
}
