# mcp-hub

A governed MCP marketplace for Claude Code: browse an approved catalogue, enable a server in one click
(or by asking Claude Code), and get a verified install, short-lived injected credentials and an audit log —
with no secrets in your Claude config.

> Work in progress — built in a 3-hour timebox. See [`evidence/`](evidence/) for intent, design and decisions.

## Verify

```sh
scripts/verify.sh            # lint, gosec, race tests + coverage gate, govulncheck, gitleaks, UI checks
scripts/verify.sh --mutation # + mutation testing on changed packages
```
