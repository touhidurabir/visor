import { describe, expect, it } from 'vitest'
import { connectSnippets, maskToken } from './mcp-connect'

const url = 'http://127.0.0.1:4567/mcp'
const token = 'vsr_abc123'

describe('connectSnippets', () => {
  const s = connectSnippets(url, token)

  it('gives Claude Code one command with the token', () => {
    expect(s.claude).toBe('claude mcp add --scope user --transport http visor http://127.0.0.1:4567/mcp --header "Authorization: Bearer vsr_abc123"')
  })

  it('gives Codex the variable way and the config.toml way', () => {
    expect(s.codexEnv).toBe('export VISOR_TOKEN=vsr_abc123')
    expect(s.codexAdd).toBe('codex mcp add visor --url http://127.0.0.1:4567/mcp --bearer-token-env-var VISOR_TOKEN')
    expect(s.codexToml).toBe('[mcp_servers.visor]\nurl = "http://127.0.0.1:4567/mcp"\nhttp_headers = { Authorization = "Bearer vsr_abc123" }')
  })

  it('gives other clients JSON that parses', () => {
    const j = JSON.parse(s.json)
    expect(j.mcpServers.visor.url).toBe(url)
    expect(j.mcpServers.visor.headers.Authorization).toBe('Bearer vsr_abc123')
  })
})

describe('maskToken', () => {
  it('hides the token wherever it appears', () => {
    expect(maskToken('a vsr_abc123 b vsr_abc123', token)).toBe('a vsr_•••••••• b vsr_••••••••')
    expect(maskToken('no token', '')).toBe('no token')
  })
})
