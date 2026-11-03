// Command devcatalogue writes a dev catalogue for scripts/dev-registry.sh: the
// embedded default catalogue plus a local dev build and/or remote entries. An
// entry whose id already exists replaces it. The result is validated with
// catalogue.Parse before it is written, so a bad entry fails fast.
//
//	go run ./scripts/devcatalogue -out dist/dev/catalogue.yaml \
//	  [-local-id ID -local-version V -platform GOOS-GOARCH -local-url URL -local-sha256 HEX] \
//	  [-remote ID=https://...]...
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Arif-Karim/mcp-hub/internal/catalogue"
)

const owner = "local-dev"

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "devcatalogue:", err)
		os.Exit(1)
	}
}

type remotes []string

func (r *remotes) String() string     { return strings.Join(*r, ",") }
func (r *remotes) Set(v string) error { *r = append(*r, v); return nil }

func run(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("devcatalogue", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "catalogue file to write")
	id := fs.String("local-id", "", "id of the local dev build")
	version := fs.String("local-version", "0.0.0-dev", "version of the local dev build")
	platform := fs.String("platform", "", "artifact platform, GOOS-GOARCH")
	url := fs.String("local-url", "", "artifact url")
	sum := fs.String("local-sha256", "", "artifact sha256")
	var rs remotes
	fs.Var(&rs, "remote", "remote entry ID=https://url (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if *out == "" {
		return errors.New("-out is required")
	}

	base, err := catalogue.DefaultCatalogue()
	if err != nil {
		return err
	}
	var extra []catalogue.Entry
	if *id != "" {
		extra = append(extra, localEntry(base, *id, *version, *platform, catalogue.Artifact{URL: *url, SHA256: *sum}))
	}
	for _, r := range rs {
		rid, rurl, ok := strings.Cut(r, "=")
		if !ok {
			return fmt.Errorf("-remote %q: want ID=https://url", r)
		}
		extra = append(extra, remoteEntry(rid, rurl))
	}

	data, err := yaml.Marshal(catalogue.Merge(base, extra...))
	if err != nil {
		return err
	}
	if _, err := catalogue.Parse(data); err != nil {
		return err
	}
	data = append([]byte("# Dev catalogue written by scripts/dev-registry.sh: the built-in catalogue plus dev entries. Not for production.\n"), data...)
	if err := os.WriteFile(*out, data, 0o600); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stderr, "wrote %s (%d entries)\n", *out, len(extra))
	return err
}

// localEntry builds a dev entry for a local build. When it overrides an existing
// listing, the name, tools and tags are kept so the UI still looks familiar.
func localEntry(base *catalogue.Catalogue, id, version, platform string, a catalogue.Artifact) catalogue.Entry {
	e := catalogue.Entry{
		ID:             id,
		Name:           id,
		Description:    "Local dev build of " + id,
		Owner:          owner,
		Version:        version,
		Kind:           catalogue.KindLocal,
		Classification: catalogue.ClassInternal,
		Approval:       catalogue.ApprovalApproved,
		Auth:           catalogue.AuthHubJWT,
		Tags:           []string{},
		Tools:          []catalogue.Tool{},
		Artifacts:      map[string]catalogue.Artifact{platform: a},
	}
	for _, old := range base.Entries {
		if old.ID == id {
			e.Name, e.Tools, e.Tags = old.Name, old.Tools, old.Tags
			e.Description = old.Description + " (local dev build)"
		}
	}
	return e
}

func remoteEntry(id, url string) catalogue.Entry {
	return catalogue.Entry{
		ID:             id,
		Name:           id,
		Description:    "Remote MCP server " + id + " (local dev entry)",
		Owner:          owner,
		Version:        "dev",
		Kind:           catalogue.KindRemote,
		Classification: catalogue.ClassPublic,
		Approval:       catalogue.ApprovalApproved,
		Auth:           catalogue.AuthNone,
		Tags:           []string{},
		Tools:          []catalogue.Tool{},
		URL:            url,
	}
}
