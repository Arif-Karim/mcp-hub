import { ApiError } from '../api/client'

/**
 * Maps an API failure to a sentence a developer can act on. Pass `id` only for catalogue/server
 * actions: it personalises messages and enables server-specific wording (e.g. for not_found).
 */
export function errorMessage(err: unknown, id?: string): string {
  if (!(err instanceof ApiError)) {
    return `Something went wrong: ${err instanceof Error ? err.message : String(err)}`
  }
  const name = id ? `hub__${id}` : 'that name'
  if (err.status === 404 && !id) {
    return "The daemon doesn't provide this yet (404). It may be an older mcp-hub — update it and try again."
  }
  switch (err.code) {
    case 'not_approved':
      return 'This server is not approved for use — the platform team has to approve it first.'
    case 'checksum_mismatch':
      return "Install blocked — the download didn't match the catalogue's checksum."
    case 'registry_unavailable':
      return "Couldn't reach the artifact registry. Check your network or VPN and try again."
    case 'config_conflict':
      return `You already have a server called ${name} in your Claude config that mcp-hub didn't create — not overwritten.`
    case 'config_invalid':
      return "Your ~/.claude.json couldn't be parsed — mcp-hub won't edit it until it's fixed."
    case 'unsupported_platform':
      return "This server doesn't publish a build for your platform (OS/architecture)."
    case 'start_failed':
      return 'The server was installed but failed to start. Check the daemon logs for details.'
    case 'not_found':
      return 'That server is no longer in the catalogue. Refresh to see the latest list.'
    case 'network':
      return "Can't reach the mcp-hub daemon. Is it running? Try `mcp-hub serve`."
    default:
      return `Something went wrong: ${err.message}`
  }
}
