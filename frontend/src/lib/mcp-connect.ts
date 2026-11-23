// The commands and config that connect an AI tool to Visor's MCP server,
// with the token filled in (the MCP server page's copy-boxes).

export interface ConnectSnippets {
  claude: string
  codexEnv: string
  codexAdd: string
  codexToml: string
  json: string
}

export function connectSnippets(url: string, token: string): ConnectSnippets {
  return {
    claude: `claude mcp add --scope user --transport http visor ${url} --header "Authorization: Bearer ${token}"`,
    codexEnv: `export VISOR_TOKEN=${token}`,
    codexAdd: `codex mcp add visor --url ${url} --bearer-token-env-var VISOR_TOKEN`,
    codexToml: `[mcp_servers.visor]\nurl = "${url}"\nhttp_headers = { Authorization = "Bearer ${token}" }`,
    json: JSON.stringify({ mcpServers: { visor: { url, headers: { Authorization: `Bearer ${token}` } } } }, null, 2),
  }
}

/** maskToken shows text with the token hidden (the copy keeps it). */
export function maskToken(text: string, token: string): string {
  if (!token) return text
  return text.split(token).join(token.slice(0, 4) + '••••••••')
}
