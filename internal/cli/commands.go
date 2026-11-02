package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Arif-Karim/mcp-hub/internal/claudeconfig"
	"github.com/Arif-Karim/mcp-hub/internal/skillfiles"
)

// item mirrors the contract's CatalogueItem (only what the CLI prints).
type item struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Owner          string   `json:"owner"`
	Version        string   `json:"version"`
	Kind           string   `json:"kind"`
	Classification string   `json:"classification"`
	Approval       string   `json:"approval"`
	Auth           string   `json:"auth"`
	Tags           []string `json:"tags"`
	Tools          []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"tools"`
	DocsURL     string `json:"docsUrl"`
	UpstreamURL string `json:"upstreamUrl"`
	State       struct {
		Enabled    bool   `json:"enabled"`
		Status     string `json:"status"`
		ConfigName string `json:"configName"`
		ProxyURL   string `json:"proxyUrl"`
		EnabledAt  string `json:"enabledAt"`
		LastError  string `json:"lastError"`
	} `json:"state"`
}

type itemList struct {
	Items []item `json:"items"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

func checkID(id string) error {
	if !idPattern.MatchString(id) {
		return usageErr("invalid server id %q (lowercase letters, digits and dashes)", id)
	}
	return nil
}

func search(ctx context.Context, env Env, o opts) error {
	q := url.Values{}
	query := strings.Join(o.args, " ")
	if query != "" {
		q.Set("q", query)
	}
	if o.kind != "" {
		q.Set("kind", o.kind)
	}
	body, err := env.request(ctx, o, http.MethodGet, "/catalogue", q, readTimeout)
	if err != nil {
		return err
	}
	if o.json {
		writeRaw(env.Stdout, body)
		return nil
	}
	var l itemList
	if err := decode(body, &l); err != nil {
		return err
	}
	if len(l.Items) == 0 {
		if query == "" {
			fmt.Fprintln(env.Stdout, "No catalogue entries.")
		} else {
			fmt.Fprintf(env.Stdout, "No catalogue entries match %q.\n", query)
		}
		return nil
	}
	tw := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tKIND\tAPPROVAL\tCLASSIFICATION\tENABLED\tOWNER")
	for _, it := range l.Items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			it.ID, it.Name, it.Kind, it.Approval, it.Classification, yesNo(it.State.Enabled), it.Owner)
	}
	return tw.Flush()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func info(ctx context.Context, env Env, o opts) error {
	id := o.args[0]
	if err := checkID(id); err != nil {
		return err
	}
	body, err := env.request(ctx, o, http.MethodGet, "/catalogue/"+id, nil, readTimeout)
	if err != nil {
		return err
	}
	if o.json {
		writeRaw(env.Stdout, body)
		return nil
	}
	var it item
	if err := decode(body, &it); err != nil {
		return err
	}
	printItem(env.Stdout, it)
	return nil
}

func printItem(w io.Writer, it item) {
	fmt.Fprintf(w, "%s (%s) v%s\n", it.Name, it.ID, it.Version)
	fmt.Fprintf(w, "  %s\n\n", it.Description)
	fmt.Fprintf(w, "Owner:           %s\n", it.Owner)
	fmt.Fprintf(w, "Kind:            %s\n", describeKind(it.Kind))
	fmt.Fprintf(w, "Classification:  %s (highest data classification it can return)\n", it.Classification)
	fmt.Fprintf(w, "Approval:        %s\n", describeApproval(it.Approval))
	fmt.Fprintf(w, "Auth:            %s\n", describeAuth(it.Auth, it.ID))
	if len(it.Tags) > 0 {
		fmt.Fprintf(w, "Tags:            %s\n", strings.Join(it.Tags, ", "))
	}
	if it.UpstreamURL != "" {
		fmt.Fprintf(w, "Upstream:        %s\n", it.UpstreamURL)
	}
	if it.DocsURL != "" {
		fmt.Fprintf(w, "Docs:            %s\n", it.DocsURL)
	}
	fmt.Fprintln(w, "\nTools:")
	if len(it.Tools) == 0 {
		fmt.Fprintln(w, "  (none listed)")
	}
	for _, t := range it.Tools {
		fmt.Fprintf(w, "  - %s: %s\n", t.Name, t.Description)
	}
	fmt.Fprintln(w)
	if !it.State.Enabled {
		fmt.Fprintf(w, "State:           not enabled (would be added to Claude Code as %s)\n", it.State.ConfigName)
	} else {
		fmt.Fprintf(w, "State:           enabled (%s)\n", it.State.Status)
		fmt.Fprintf(w, "Config name:     %s\n", it.State.ConfigName)
		fmt.Fprintf(w, "Proxy URL:       %s\n", it.State.ProxyURL)
		if it.State.EnabledAt != "" {
			fmt.Fprintf(w, "Enabled at:      %s\n", it.State.EnabledAt)
		}
	}
	if it.State.LastError != "" {
		fmt.Fprintf(w, "Last error:      %s\n", it.State.LastError)
	}
}

func describeKind(k string) string {
	switch k {
	case "local":
		return "local (downloaded, sha256-verified and run on your machine by mcp-hub)"
	case "remote":
		return "remote (hosted service, reached through the mcp-hub proxy)"
	}
	return k
}

func describeApproval(a string) string {
	switch a {
	case "pending":
		return "pending (announced by the owning team, not yet approved; it cannot be enabled yet)"
	case "revoked":
		return "revoked (withdrawn by governance; it cannot be enabled)"
	}
	return a
}

func describeAuth(a, id string) string {
	switch a {
	case "hub-jwt":
		return fmt.Sprintf("mcp-hub injects a short-lived signed token (audience %q) on every request; "+
			"no secrets are stored in your Claude config", id)
	case "none":
		return "No credentials are sent to the server"
	}
	return a
}

func enable(ctx context.Context, env Env, o opts) error {
	it, err := toggle(ctx, env, o, "enable", enableTimeout)
	if err != nil || it == nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Enabled %s as %s (status: %s).\n", it.Name, it.State.ConfigName, it.State.Status)
	if it.State.ProxyURL != "" {
		fmt.Fprintf(env.Stdout, "Proxy URL: %s\n", it.State.ProxyURL)
	}
	fmt.Fprintln(env.Stdout, "Run /reload-plugins in Claude Code (or restart it) to load the new server.")
	return nil
}

func disable(ctx context.Context, env Env, o opts) error {
	it, err := toggle(ctx, env, o, "disable", disableTimeout)
	if err != nil || it == nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Disabled %s: removed %s from your Claude Code config.\n", it.Name, it.State.ConfigName)
	fmt.Fprintln(env.Stdout, "Run /reload-plugins in Claude Code (or restart it) to unload the server.")
	return nil
}

// toggle POSTs enable/disable. It returns a nil item when --json already
// printed the raw response (success or error).
func toggle(ctx context.Context, env Env, o opts, action string, timeout time.Duration) (*item, error) {
	id := o.args[0]
	if err := checkID(id); err != nil {
		return nil, err
	}
	body, err := env.request(ctx, o, http.MethodPost, "/servers/"+id+"/"+action, nil, timeout)
	if err != nil {
		return nil, err
	}
	if o.json {
		writeRaw(env.Stdout, body)
		return nil, nil
	}
	var it item
	if err := decode(body, &it); err != nil {
		return nil, err
	}
	return &it, nil
}

type statusBody struct {
	Version          string `json:"version"`
	Addr             string `json:"addr"`
	ClaudeConfigPath string `json:"claudeConfigPath"`
	EnabledCount     int    `json:"enabledCount"`
	CatalogueSource  string `json:"catalogueSource"`
	User             string `json:"user"`
}

func status(ctx context.Context, env Env, o opts) error {
	body, err := env.request(ctx, o, http.MethodGet, "/status", nil, readTimeout)
	if err != nil {
		return err
	}
	if o.json {
		writeRaw(env.Stdout, body)
		return nil
	}
	var s statusBody
	if err := decode(body, &s); err != nil {
		return err
	}
	catBody, err := env.call(ctx, http.MethodGet, "/catalogue", nil, readTimeout)
	if err != nil {
		return err
	}
	var l itemList
	if err := decode(catBody, &l); err != nil {
		return err
	}
	w := env.Stdout
	fmt.Fprintf(w, "mcp-hub %s on %s\n", s.Version, s.Addr)
	fmt.Fprintf(w, "User:            %s\n", s.User)
	fmt.Fprintf(w, "Catalogue:       %s\n", s.CatalogueSource)
	fmt.Fprintf(w, "Claude config:   %s\n", s.ClaudeConfigPath)
	fmt.Fprintf(w, "Enabled servers: %d\n", s.EnabledCount)
	for _, it := range l.Items {
		if it.State.Enabled {
			fmt.Fprintf(w, "  %s  %s (%s)\n", it.State.ConfigName, it.Name, it.State.Status)
		}
	}
	return nil
}

func uninstallConfig(_ context.Context, env Env, _ opts) error {
	w, path, err := env.Config()
	if err != nil {
		return fmt.Errorf("locating Claude config: %w", err)
	}
	owned, err := w.Owned()
	if err != nil {
		return err
	}
	var skipped []string
	var partial *claudeconfig.PartialRemoveError
	if err := w.RemoveAll(); errors.As(err, &partial) {
		skipped = partial.Skipped
	} else if err != nil {
		return err
	}
	var removed []string
	for _, o := range owned {
		if !slices.Contains(skipped, o.ConfigName) {
			removed = append(removed, o.ConfigName)
		}
	}
	slices.Sort(removed)
	if len(removed) == 0 && len(skipped) == 0 {
		fmt.Fprintln(env.Stdout, "No mcp-hub entries in your Claude Code config; nothing to remove.")
		return nil
	}
	if len(removed) > 0 {
		fmt.Fprintf(env.Stdout, "Removed %s from %s.\n", strings.Join(removed, ", "), path)
	}
	for _, name := range skipped {
		fmt.Fprintf(env.Stderr, "Kept %s: it was modified outside mcp-hub, so it was left in place. Remove it yourself if you no longer need it.\n", name)
	}
	return nil
}

// installSkill writes the skill embedded in this binary; it needs no daemon,
// so install.sh can run it before the daemon starts.
func installSkill(_ context.Context, env Env, _ opts) error {
	dir, err := env.ClaudeDir()
	if err != nil {
		return fmt.Errorf("locating Claude Code directory: %w", err)
	}
	if err := skillfiles.Install(dir); err != nil {
		return fmt.Errorf("installing skill: %w", err)
	}
	skills := skillfiles.Skills()
	fmt.Fprintf(env.Stdout, "Installed %d mcp-hub Claude Code skills to %s:\n", len(skills), skillfiles.SkillsDir(dir))
	for _, s := range skills {
		fmt.Fprintf(env.Stdout, "  %s  %s\n", s.Name, skillfiles.SkillPath(dir, s.Name))
	}
	fmt.Fprintln(env.Stdout, "Restart Claude Code or run /reload-plugins to load them.")
	return nil
}
