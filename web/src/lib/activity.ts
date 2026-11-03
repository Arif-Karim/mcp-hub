// Audit `method` is the JSON-RPC method (batches joined with ","), or the HTTP
// method for non-JSON-RPC requests such as the GET that opens the SSE stream.

const methods = (method: string): string[] => method.split(',')

/** True when the entry is real tool use rather than connection traffic. */
export const isToolCall = (method: string): boolean => methods(method).includes('tools/call')

const labels: Record<string, string> = {
  initialize: 'Handshake',
  'notifications/initialized': 'Connection ready',
  'tools/list': 'Listed tools',
  'resources/list': 'Listed resources',
  'resources/templates/list': 'Listed resource templates',
  'prompts/list': 'Listed prompts',
  ping: 'Ping',
  GET: 'Event stream',
  DELETE: 'Disconnected',
  'tools/call': 'Tool call',
}

/** A human-friendly label for an audit method; unknown methods are shown as-is. */
export const activityLabel = (method: string): string =>
  methods(method)
    .map((m) => labels[m] ?? m)
    .join(', ')
