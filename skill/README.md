# mcp-hub Claude Code skill

`skill/mcp-hub/SKILL.md` teaches Claude Code to use the `mcp-hub` CLI, so a developer can say
"find me an MCP server for service ownership" or "connect Claude to Jira" and Claude will search the
governed catalogue, explain the options (owner, classification, approval, tools), ask for confirmation
and run `mcp-hub enable <id>`.

Guardrails written into the skill:

- only `approved` entries are offered for enabling; `pending`/`revoked` are explained, never attempted;
- the user confirms before any enable or disable;
- errors (`checksum_mismatch`, `config_conflict`, daemon down, ...) are explained in plain words;
- `allowed-tools: Bash(mcp-hub:*)` limits the skill to the `mcp-hub` command.

## Install

`install.sh` copies `skill/mcp-hub/` from the release tarball to `~/.claude/skills/mcp-hub/`
(user scope, available in every project). `install.sh --uninstall` removes it. To try it from a
checkout without the installer:

```sh
mkdir -p ~/.claude/skills && cp -R skill/mcp-hub ~/.claude/skills/
```

Restart Claude Code after installing so it picks the skill up.
