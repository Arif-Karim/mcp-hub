package catalogue

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
	platformPattern = regexp.MustCompile(`^(darwin|linux)-(arm64|amd64)$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Parse strictly decodes a catalogue document (unknown fields are errors) and validates it.
func Parse(data []byte) (*Catalogue, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Catalogue
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if err := Validate(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks every rule and returns all problems joined, each wrapping
// ErrInvalid and naming the entry id and field.
func Validate(c *Catalogue) error {
	var errs []error
	if c.Version != 1 {
		errs = append(errs, fmt.Errorf("%w: version: must be 1, got %d", ErrInvalid, c.Version))
	}
	seen := map[string]bool{}
	for _, e := range c.Entries {
		bad := func(field, format string, args ...any) {
			errs = append(errs, fmt.Errorf("%w: entry %q: %s: %s", ErrInvalid, e.ID, field, fmt.Sprintf(format, args...)))
		}
		switch {
		case !idPattern.MatchString(e.ID):
			bad("id", "must match %s", idPattern)
		case seen[e.ID]:
			bad("id", "duplicate")
		}
		seen[e.ID] = true
		required := []struct{ field, value string }{
			{"name", e.Name}, {"description", e.Description}, {"owner", e.Owner}, {"version", e.Version},
		}
		for _, r := range required {
			if strings.TrimSpace(r.value) == "" {
				bad(r.field, "required")
			}
		}
		if e.Classification != ClassPublic && e.Classification != ClassInternal && e.Classification != ClassConfidential {
			bad("classification", "unknown value %q", e.Classification)
		}
		if e.Approval != ApprovalApproved && e.Approval != ApprovalPending && e.Approval != ApprovalRevoked {
			bad("approval", "unknown value %q", e.Approval)
		}
		if e.Auth != AuthNone && e.Auth != AuthHubJWT {
			bad("auth", "unknown value %q", e.Auth)
		}
		switch e.Kind {
		case KindRemote:
			validateRemote(e, bad)
		case KindLocal:
			validateLocal(e, bad)
		default:
			bad("kind", "unknown value %q", e.Kind)
		}
	}
	return errors.Join(errs...)
}

type reporter func(field, format string, args ...any)

func validateRemote(e Entry, bad reporter) {
	if u, err := url.Parse(e.URL); err != nil || u.Scheme != "https" || u.Host == "" {
		bad("url", "remote entries need an https:// url")
	}
	if len(e.Artifacts) > 0 {
		bad("artifacts", "must be empty for remote entries")
	}
	// Hub tokens are only for servers the hub runs. Forwarding one to a third-party
	// remote would hand a bearer credential to a party outside our trust boundary,
	// and nothing guarantees that server checks the audience (the entry id), so a
	// leaked token could be replayed against our own servers.
	if e.Auth == AuthHubJWT {
		bad("auth", "remote entries must use auth: none")
	}
}

func validateLocal(e Entry, bad reporter) {
	if e.URL != "" {
		bad("url", "must be empty for local entries")
	}
	if e.Approval == ApprovalApproved && len(e.Artifacts) == 0 {
		bad("artifacts", "approved local entries need at least one artifact")
	}
	for _, p := range slices.Sorted(maps.Keys(e.Artifacts)) { // sorted: stable error output
		a := e.Artifacts[p]
		field := fmt.Sprintf("artifacts[%q]", p)
		if !platformPattern.MatchString(p) {
			bad(field, "platform must match %s", platformPattern)
		}
		if !artifactURLOK(a.URL) {
			bad(field+".url", "must be https (http only for loopback test registries)")
		}
		if !sha256Pattern.MatchString(a.SHA256) {
			bad(field+".sha256", "must be 64 lowercase hex characters")
		}
	}
}

func artifactURLOK(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		h := u.Hostname()
		return h == "127.0.0.1" || h == "localhost"
	}
	return false
}
