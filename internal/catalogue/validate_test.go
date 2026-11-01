package catalogue_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
)

const sha = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func validLocal() catalogue.Entry {
	return catalogue.Entry{
		ID: "svc-dir", Name: "Svc", Description: "d", Owner: "o", Version: "0.1.0",
		Kind: catalogue.KindLocal, Classification: catalogue.ClassInternal,
		Approval: catalogue.ApprovalApproved, Auth: catalogue.AuthHubJWT,
		Artifacts: map[string]catalogue.Artifact{
			"darwin-arm64": {URL: "https://example.test/a.tar.gz", SHA256: sha},
		},
	}
}

func validRemote() catalogue.Entry {
	return catalogue.Entry{
		ID: "remote-one", Name: "R", Description: "d", Owner: "o", Version: "1",
		Kind: catalogue.KindRemote, Classification: catalogue.ClassPublic,
		Approval: catalogue.ApprovalApproved, Auth: catalogue.AuthNone,
		URL: "https://mcp.example.test/mcp",
	}
}

func TestValidateAcceptsValidCatalogue(t *testing.T) {
	c := &catalogue.Catalogue{Version: 1, Entries: []catalogue.Entry{validLocal(), validRemote()}}
	if err := catalogue.Validate(c); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(c *catalogue.Catalogue)
		want   string // substring that must appear in the error
	}{
		{"version", func(c *catalogue.Catalogue) { c.Version = 2 }, "version"},
		{"id pattern", func(c *catalogue.Catalogue) { c.Entries[0].ID = "Bad_ID" }, `"Bad_ID": id`},
		{"id too short", func(c *catalogue.Catalogue) { c.Entries[0].ID = "a" }, `"a": id`},
		{"id leading dash", func(c *catalogue.Catalogue) { c.Entries[0].ID = "-ab" }, `"-ab": id`},
		{"id too long", func(c *catalogue.Catalogue) { c.Entries[0].ID = strings.Repeat("a", 64) }, ": id"},
		{"duplicate id", func(c *catalogue.Catalogue) { c.Entries[1].ID = "svc-dir" }, `"svc-dir": id: duplicate`},
		{"name", func(c *catalogue.Catalogue) { c.Entries[0].Name = " " }, `"svc-dir": name`},
		{"description", func(c *catalogue.Catalogue) { c.Entries[0].Description = "" }, `"svc-dir": description`},
		{"owner", func(c *catalogue.Catalogue) { c.Entries[0].Owner = "" }, `"svc-dir": owner`},
		{"entry version", func(c *catalogue.Catalogue) { c.Entries[0].Version = "" }, `"svc-dir": version`},
		{"kind", func(c *catalogue.Catalogue) { c.Entries[0].Kind = "cloud" }, `"svc-dir": kind`},
		{"classification", func(c *catalogue.Catalogue) { c.Entries[0].Classification = "secret" }, `"svc-dir": classification`},
		{"approval", func(c *catalogue.Catalogue) { c.Entries[0].Approval = "maybe" }, `"svc-dir": approval`},
		{"auth", func(c *catalogue.Catalogue) { c.Entries[0].Auth = "basic" }, `"svc-dir": auth`},
		{"remote url missing", func(c *catalogue.Catalogue) { c.Entries[1].URL = "" }, `"remote-one": url`},
		{"remote url http", func(c *catalogue.Catalogue) { c.Entries[1].URL = "http://mcp.example.test/mcp" }, `"remote-one": url`},
		{"remote url no host", func(c *catalogue.Catalogue) { c.Entries[1].URL = "https:///mcp" }, `"remote-one": url`},
		{"remote with artifacts", func(c *catalogue.Catalogue) {
			c.Entries[1].Artifacts = map[string]catalogue.Artifact{"linux-amd64": {URL: "https://x.test/a", SHA256: sha}}
		}, `"remote-one": artifacts`},
		{"remote hub-jwt", func(c *catalogue.Catalogue) { c.Entries[1].Auth = catalogue.AuthHubJWT }, `"remote-one": auth`},
		{"local with url", func(c *catalogue.Catalogue) { c.Entries[0].URL = "https://x.test/mcp" }, `"svc-dir": url`},
		{"approved local without artifacts", func(c *catalogue.Catalogue) { c.Entries[0].Artifacts = nil }, `"svc-dir": artifacts`},
		{"artifact platform", func(c *catalogue.Catalogue) {
			c.Entries[0].Artifacts["windows-amd64"] = catalogue.Artifact{URL: "https://x.test/a", SHA256: sha}
		}, `"svc-dir": artifacts["windows-amd64"]`},
		{"artifact http", func(c *catalogue.Catalogue) {
			c.Entries[0].Artifacts["linux-amd64"] = catalogue.Artifact{URL: "http://example.test/a", SHA256: sha}
		}, `"svc-dir": artifacts["linux-amd64"].url`},
		{"artifact bad url", func(c *catalogue.Catalogue) {
			c.Entries[0].Artifacts["linux-amd64"] = catalogue.Artifact{URL: "://nope", SHA256: sha}
		}, `artifacts["linux-amd64"].url`},
		{"artifact sha short", func(c *catalogue.Catalogue) {
			c.Entries[0].Artifacts["linux-amd64"] = catalogue.Artifact{URL: "https://x.test/a", SHA256: sha[:63]}
		}, `artifacts["linux-amd64"].sha256`},
		{"artifact sha upper", func(c *catalogue.Catalogue) {
			c.Entries[0].Artifacts["linux-amd64"] = catalogue.Artifact{URL: "https://x.test/a", SHA256: strings.ToUpper(sha)}
		}, `artifacts["linux-amd64"].sha256`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &catalogue.Catalogue{Version: 1, Entries: []catalogue.Entry{validLocal(), validRemote()}}
			tc.mutate(c)
			err := catalogue.Validate(c)
			if !errors.Is(err, catalogue.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestValidateAllowsLoopbackHTTPArtifacts(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:8080/a.tar.gz", "http://localhost/a.tar.gz"} {
		e := validLocal()
		e.Artifacts["linux-arm64"] = catalogue.Artifact{URL: u, SHA256: sha}
		if err := catalogue.Validate(&catalogue.Catalogue{Version: 1, Entries: []catalogue.Entry{e}}); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
}

func TestValidateAllowsPendingLocalWithoutArtifacts(t *testing.T) {
	e := validLocal()
	e.Approval = catalogue.ApprovalRevoked
	e.Artifacts = nil
	if err := catalogue.Validate(&catalogue.Catalogue{Version: 1, Entries: []catalogue.Entry{e}}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	a, b := validLocal(), validRemote()
	a.Owner = ""
	b.URL = ""
	err := catalogue.Validate(&catalogue.Catalogue{Version: 1, Entries: []catalogue.Entry{a, b}})
	for _, want := range []string{`"svc-dir": owner`, `"remote-one": url`} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}
}

func TestParseIsStrict(t *testing.T) {
	_, err := catalogue.Parse([]byte("version: 1\nentries: []\nsurprise: true\n"))
	if !errors.Is(err, catalogue.ErrInvalid) || !strings.Contains(err.Error(), "surprise") {
		t.Fatalf("err = %v, want ErrInvalid naming the unknown field", err)
	}
	if _, err := catalogue.Parse(nil); !errors.Is(err, catalogue.ErrInvalid) {
		t.Fatalf("empty input: err = %v, want ErrInvalid", err)
	}
}

func TestParseValidates(t *testing.T) {
	_, err := catalogue.Parse([]byte("version: 3\nentries: []\n"))
	if !errors.Is(err, catalogue.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	c, err := catalogue.Parse([]byte("version: 1\nentries: []\n"))
	if err != nil || c.Version != 1 {
		t.Fatalf("Parse = %+v, %v", c, err)
	}
}

func TestSampleInvalidCatalogueIsRejected(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "sample-data", "invalid-catalogue.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = catalogue.Parse(data)
	if !errors.Is(err, catalogue.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if n := strings.Count(err.Error(), "\n") + 1; n < 3 {
		t.Fatalf("want several problems reported, got %d: %v", n, err)
	}
}
