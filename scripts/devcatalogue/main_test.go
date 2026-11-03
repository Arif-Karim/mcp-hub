package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
)

const sum = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func load(t *testing.T, path string) *catalogue.Catalogue {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test temp file
	if err != nil {
		t.Fatal(err)
	}
	c, err := catalogue.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func find(c *catalogue.Catalogue, id string) (catalogue.Entry, int) {
	n := 0
	var got catalogue.Entry
	for _, e := range c.Entries {
		if e.ID == id {
			got = e
			n++
		}
	}
	return got, n
}

func TestRunAddsLocalAndRemoteToDefault(t *testing.T) {
	out := filepath.Join(t.TempDir(), "catalogue.yaml")
	err := run([]string{"-out", out,
		"-local-id", "leave-balance", "-local-version", "1.2.3", "-platform", "darwin-arm64",
		"-local-url", "http://127.0.0.1:7421/leave-balance.tar.gz", "-local-sha256", sum,
		"-remote", "cf-docs=https://docs.example.com/mcp",
		"-remote", "other=https://other.example.com/mcp"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	c := load(t, out)
	def, _ := catalogue.DefaultCatalogue()
	for _, d := range def.Entries {
		if _, n := find(c, d.ID); n != 1 {
			t.Errorf("default entry %s appears %d times", d.ID, n)
		}
	}
	l, n := find(c, "leave-balance")
	if n != 1 {
		t.Fatalf("leave-balance appears %d times", n)
	}
	if l.Name != "leave-balance" || l.Description != "Local dev build of leave-balance" || l.Owner != "local-dev" ||
		l.Version != "1.2.3" || l.Kind != catalogue.KindLocal || l.Classification != catalogue.ClassInternal ||
		l.Approval != catalogue.ApprovalApproved || l.Auth != catalogue.AuthHubJWT || len(l.Tools) != 0 {
		t.Fatalf("local entry %+v", l)
	}
	a := l.Artifacts["darwin-arm64"]
	if len(l.Artifacts) != 1 || a.SHA256 != sum || a.URL != "http://127.0.0.1:7421/leave-balance.tar.gz" {
		t.Fatalf("artifacts %+v", l.Artifacts)
	}
	r, _ := find(c, "cf-docs")
	if r.Kind != catalogue.KindRemote || r.URL != "https://docs.example.com/mcp" || r.Auth != catalogue.AuthNone ||
		r.Approval != catalogue.ApprovalApproved || r.Classification != catalogue.ClassPublic ||
		r.Name != "cf-docs" || r.Owner != "local-dev" || r.Version != "dev" || len(r.Artifacts) != 0 ||
		r.Description != "Remote MCP server cf-docs (local dev entry)" {
		t.Fatalf("remote entry %+v", r)
	}
	if _, n := find(c, "other"); n != 1 {
		t.Fatal("second remote missing")
	}
}

func TestRunOverrideKeepsListingMetadata(t *testing.T) {
	out := filepath.Join(t.TempDir(), "catalogue.yaml")
	err := run([]string{"-out", out, "-local-id", "service-directory", "-local-version", "0.0.0-dev",
		"-platform", "linux-amd64", "-local-url", "http://127.0.0.1:1/x.tar.gz", "-local-sha256", sum}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	e, n := find(load(t, out), "service-directory")
	if n != 1 || e.Version != "0.0.0-dev" || e.Artifacts["linux-amd64"].SHA256 != sum {
		t.Fatalf("override %+v (%d)", e, n)
	}
	if e.Name != "Service Directory" || len(e.Tools) != 2 || len(e.Tags) != 3 ||
		!strings.HasSuffix(e.Description, "(local dev build)") || e.Owner != "local-dev" || e.DocsURL != "" {
		t.Fatalf("override metadata %+v", e)
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	cases := map[string][]string{
		"http remote":    {"-remote", "x1=http://example.com/mcp"},
		"remote no =":    {"-remote", "x1"},
		"bad remote id":  {"-remote", "X=https://example.com/mcp"},
		"bad local id":   {"-local-id", "Bad_ID", "-platform", "linux-amd64", "-local-url", "http://127.0.0.1:1/x", "-local-sha256", sum},
		"bad sha":        {"-local-id", "abc", "-platform", "linux-amd64", "-local-url", "http://127.0.0.1:1/x", "-local-sha256", "nope"},
		"unknown flag":   {"-nope"},
		"positional arg": {"extra"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "catalogue.yaml")
			if err := run(append([]string{"-out", out}, args...), &bytes.Buffer{}); err == nil {
				t.Fatal("want error")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("catalogue written despite error")
			}
		})
	}
}

func TestRunRequiresOut(t *testing.T) {
	if err := run(nil, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "-out") {
		t.Fatalf("got %v", err)
	}
}

func TestRunReportsWrite(t *testing.T) {
	out := filepath.Join(t.TempDir(), "catalogue.yaml")
	var buf bytes.Buffer
	if err := run([]string{"-out", out}, &buf); err != nil {
		t.Fatal(err)
	}
	def, _ := catalogue.DefaultCatalogue()
	if len(load(t, out).Entries) != len(def.Entries) || !strings.Contains(buf.String(), out) {
		t.Fatalf("output %q", buf.String())
	}
}
