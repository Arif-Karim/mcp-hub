/** Static content for the /publish page, condensed from docs/publishing.md (the source of truth). */
import type { Approval } from '../api/types'
import { blockedReason } from './labels'

export const GUIDE_URL = 'https://github.com/Arif-Karim/mcp-hub/blob/main/docs/publishing.md'
export const TEMPLATE_URL = 'https://github.com/Arif-Karim/mcp-hub/tree/main/examples/mcp-server-template'

export const kinds = [
  {
    kind: 'Local',
    does: "Downloads your release, checks its sha256, runs it on the engineer's laptop behind the proxy, and injects a short-lived token (at most 15 minutes) naming the user.",
    when: 'Your server calls internal APIs on the user’s behalf.',
  },
  {
    kind: 'Remote',
    does: 'Points Claude Code at your hosted HTTPS endpoint through the proxy, with no hub token forwarded.',
    when: 'You already run a hosted MCP service.',
  },
]

export const steps: { title: string; body: string; code?: string }[] = [
  {
    title: 'Build from the template',
    body: 'Copy the reference template — its README is the standard. Replace the tools in internal/directory, keep the rest, and use the verified sub (the engineer’s identity) when you call your own APIs.',
  },
  {
    title: 'Release it',
    body: 'One .tar.gz per platform with an executable named mcp-server at the archive root, plus checksums.txt (sha256). The template’s workflow builds these and prints a ready-to-paste artifacts: snippet.',
  },
  {
    title: 'Open a catalogue PR',
    body: 'Add your entry to internal/catalogue/default.yaml with',
    code: 'approval: pending',
  },
  {
    title: 'Platform team review',
    body: 'Platform team checks ownership, an honest data classification, that the tools match what the server does, the template standard, and that the checksums match the release.',
  },
  {
    title: 'Approved and live',
    body: 'The platform team flips the entry to approved, and engineers can enable it from this UI or from Claude Code.',
  },
]

export const lifecycle: { state: Approval; seen: string; who: string }[] = [
  {
    state: 'pending',
    seen: `“${blockedReason.pending}” — visible so other teams know it’s on the way; enable is refused.`,
    who: 'Your team, in the PR',
  },
  { state: 'approved', seen: 'Engineers can enable it.', who: 'Platform team, after review' },
  {
    state: 'revoked',
    seen: `“${blockedReason.revoked}” — enable is refused; enabled copies are disabled when the daemon restarts.`,
    who: 'Platform team (incident, end of life)',
  },
]

export const exampleEntry = `  - id: my-service              # ^[a-z0-9][a-z0-9-]{1,62}$, also the token audience
    name: My Service
    description: One sentence on what it lets Claude do.
    owner: my-team
    version: 0.1.0
    kind: local
    classification: internal    # public | internal | confidential (highest data it can return)
    approval: pending           # platform team flips this to approved
    auth: hub-jwt               # local servers verify the hub token
    tags: [my-domain]
    tools:
      - name: do_thing
        description: What the tool does.
    docsUrl: https://github.com/<org>/<repo>      # https only
    artifacts:
      darwin-arm64:
        url: https://github.com/<org>/<repo>/releases/download/my-service-v0.1.0/my-service_darwin_arm64.tar.gz
        sha256: "<from checksums.txt>"
      # darwin-amd64, linux-amd64 likewise
`
