// The MCP server's status, followed once for the whole window (the sidebar's
// dot), from Get and the visor:mcp event.
import { Events } from '@wailsio/runtime'
import { ref } from 'vue'
import { VisorMcp } from '../../bindings/events'
import { Status } from '../../bindings/github.com/touhidurabir/visor/internal/app/mcpservice'
import type { McpView } from '../../bindings/github.com/touhidurabir/visor/internal/app/models'

export const mcpStatus = ref('')
let following = false

export function followMcp() {
  if (following) return
  following = true
  Events.On(VisorMcp, (ev: { data: McpView }) => (mcpStatus.value = ev.data.Status))
  // Status, not Get: Get reads the token, which can mean a keyring prompt.
  Status()
    .then((st) => (mcpStatus.value = st))
    .catch(() => {})
}
