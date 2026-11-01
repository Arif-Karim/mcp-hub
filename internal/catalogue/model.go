// Package catalogue defines the MCP server catalogue: the governed list of
// servers developers may enable. The model mirrors api/openapi.yaml.
package catalogue

// Kind says where an MCP server runs.
type Kind string

const (
	// KindLocal servers are downloaded from the registry and run on the developer's machine.
	KindLocal Kind = "local"
	// KindRemote servers are hosted elsewhere; enabling one only writes config.
	KindRemote Kind = "remote"
)

// Approval is the governance state of an entry. Only approved entries can be enabled.
type Approval string

// Approval states.
const (
	ApprovalApproved Approval = "approved"
	ApprovalPending  Approval = "pending"
	ApprovalRevoked  Approval = "revoked"
)

// Classification is the highest data classification the server can return.
type Classification string

// Classification levels.
const (
	ClassPublic       Classification = "public"
	ClassInternal     Classification = "internal"
	ClassConfidential Classification = "confidential"
)

// AuthMode controls what credentials the proxy attaches upstream.
type AuthMode string

const (
	// AuthNone sends no credentials (third-party/public servers).
	AuthNone AuthMode = "none"
	// AuthHubJWT injects a short-lived JWT whose audience is the entry ID.
	AuthHubJWT AuthMode = "hub-jwt"
)

// Tool is a tool the server advertises, shown in the UI before enabling.
type Tool struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
}

// Artifact is a downloadable build of a local server for one platform.
type Artifact struct {
	URL    string `yaml:"url" json:"url"`
	SHA256 string `yaml:"sha256" json:"sha256"`
}

// Entry is one catalogue listing.
type Entry struct {
	ID             string         `yaml:"id" json:"id"`
	Name           string         `yaml:"name" json:"name"`
	Description    string         `yaml:"description" json:"description"`
	Owner          string         `yaml:"owner" json:"owner"`
	Version        string         `yaml:"version" json:"version"`
	Kind           Kind           `yaml:"kind" json:"kind"`
	Classification Classification `yaml:"classification" json:"classification"`
	Approval       Approval       `yaml:"approval" json:"approval"`
	Auth           AuthMode       `yaml:"auth" json:"auth"`
	Tags           []string       `yaml:"tags" json:"tags"`
	Tools          []Tool         `yaml:"tools" json:"tools"`
	DocsURL        string         `yaml:"docsUrl,omitempty" json:"docsUrl,omitempty"`
	// URL is the upstream MCP endpoint (remote entries only).
	URL string `yaml:"url,omitempty" json:"upstreamUrl,omitempty"`
	// Artifacts maps "GOOS-GOARCH" (e.g. "darwin-arm64") to a build (local entries only).
	Artifacts map[string]Artifact `yaml:"artifacts,omitempty" json:"-"`
}

// Catalogue is the full document loaded from YAML.
type Catalogue struct {
	Version int     `yaml:"version"`
	Entries []Entry `yaml:"entries"`
}
