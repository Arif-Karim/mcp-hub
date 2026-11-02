# mcp-hub Claude Code skill

`skill/mcp-hub/SKILL.md` teaches Claude Code to use the `mcp-hub` CLI, so a developer can say
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

`install.sh` copies `skill/mcp-hub/` from the release tarball to `~/.claude/skills/mcp-hub/`
(user scope, available in every project). `install.sh --uninstall` removes it. To try it from a
checkout without the installer:

```sh
mkdir -p ~/.claude/skills && cp -R skill/mcp-hub ~/.claude/skills/
```

Restart Claude Code after installing so it picks the skill up.
