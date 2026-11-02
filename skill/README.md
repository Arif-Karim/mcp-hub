# mcp-hub Claude Code skill

The skill lives in [`internal/skillfiles/mcp-hub/SKILL.md`](../internal/skillfiles/mcp-hub/SKILL.md) and is
embedded in the `mcp-hub` binary (`go:embed`). That file is the only copy: a test fails if one reappears here.

It teaches Claude Code to use the `mcp-hub` CLI, so a developer can say
"find me an MCP server for service ownership" or "connect Claude to Jira" and Claude will search the
governed catalogue, explain the options (owner, classification, approval, tools), ask for confirmation
and run `mcp-hub enable <id>`.

Guardrails written into the skill:

- only `approved` entries are offered for enabling; `pending`/`revoked` are explained, never attempted;
- the user confirms before any enable or disable;
- errors (`checksum_mismatch`, `config_conflict`, daemon down, ...) are explained in plain words;
- `allowed-tools` pre-approves only the read-only commands (`mcp-hub search`, `info`, `status`).
  `mcp-hub enable` and `disable` change the user's Claude Code config, so they still go through Claude Code's
  own permission prompt. The confirmation does not depend only on the model following the skill's instructions.

## Install

Any of these writes `~/.claude/skills/mcp-hub/SKILL.md` (user scope, available in every project) from the
skill embedded in the binary, so the installed skill always matches the binary's version:

- `install.sh` runs `mcp-hub install-skill` (and `install.sh --uninstall` removes it);
- `mcp-hub install-skill` on its own (works offline). The directory is `$CLAUDE_HOME_DIR`, else Claude Code's own
  `$CLAUDE_CONFIG_DIR`, else `~/.claude`;
- the web UI's **Install skill** / **Update skill** banner (`POST /api/v1/skill/install`), shown when the skill
  is missing or older than the running binary.

Then run `/reload-plugins` in Claude Code (or restart it) so it picks the skill up.
