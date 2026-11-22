<script setup lang="ts">
// The lens mark, the wordmark and the notification bell; the tab's server
// (this computer, or a server with its colour) with its health and failing
// count, Dashboard, Issues and, on this computer, Add new; and Doctor (on a
// server, its report), Remote servers and Settings at the bottom. Servers open as tabs above
// (TabBar). Appearance lives in Settings.
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import AppMark from './AppMark.vue'
import NotificationBell from './NotificationBell.vue'
import { followMcp, mcpStatus } from '../lib/mcp'
import { shortcutAria, shortcutHint } from '../lib/shortcuts'
import { baseName, tabHref } from '../lib/tabs'

// `connection` is the badge for a connection problem ("Not running", "Not
// answering", "Not connected"; lib/connection.ts connectionBadge), null
// while connected.
// remote/color: the tab is a server's (its colour leads its name).
// blocked: a server Visor can't reach (its report says why): Dashboard and
// Issues are greyed until it can.
const props = defineProps<{ name: string; health: string; failing: number; openIssues: number; connection: string | null; remote?: boolean; readOnly?: boolean; color?: string; blocked?: boolean }>()

// Shortcut hints, as Visor's menu bar shows them on macOS (⌘N, ⌘,) and
// AppShell carries them out on Linux (Ctrl+N, Ctrl+,).
const KEYS = {
  newConfig: { hint: shortcutHint('newConfig'), aria: shortcutAria('newConfig') },
  settings: { hint: shortcutHint('settings'), aria: shortcutAria('settings') },
}

// Which item is the current page. A config's detail page belongs under
// Dashboard, the Issues log under Issues, and the form under Add new.
const route = useRoute()
const sections: Record<string, string[]> = {
  dashboard: ['dashboard', 'config'],
  issues: ['issues', 'issues-log'],
  activity: ['activity'],
  new: ['new', 'new-form', 'new-import'],
  servers: ['servers'],
  mcp: ['mcp'],
  doctor: ['doctor', 'report'],
  settings: ['settings'],
}
followMcp()
const mcpRunning = computed(() => mcpStatus.value === 'running')
const on = (key: string) => ({ on: sections[key].includes(baseName(route.name)) })

const dot = computed(() => {
  if (props.connection) return 'd-bad'
  return { ok: 'd-ok', warning: 'd-warn', error: 'd-bad' }[props.health] ?? 'd-idle'
})
</script>

<template>
  <nav class="sidebar" :aria-label="remote ? name : 'Servers'">
    <div class="brand">
      <span class="mark" aria-hidden="true"><AppMark /></span>
      <span class="wordmark">Visor</span>
      <NotificationBell />
    </div>

    <div class="section">
      <span class="th">{{ remote ? 'Server' : 'Servers' }}</span>
      <div class="server">
        <span v-if="remote && color" class="dot" :style="{ background: color }" aria-hidden="true" />
        <span v-else :class="['dot', dot]" aria-hidden="true" />
        <span class="server-name">{{ name }}</span>
        <span class="sr-only">health {{ health }}</span>
        <span v-if="connection" class="badge b-bad state">{{ connection }}</span>
        <span v-else-if="failing > 0" class="badge b-bad state">{{ failing }} failing</span>
      </div>
      <div class="sub">
        <RouterLink :to="tabHref('/')" :class="['nav', on('dashboard'), { dim: blocked }]" :title="blocked ? 'Visor can’t reach supervisord on this server: see Server report' : undefined">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
            <rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" />
          </svg>
          Dashboard
        </RouterLink>
        <RouterLink :to="tabHref('/issues')" :class="['nav', on('issues'), { dim: blocked }]">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" aria-hidden="true">
            <path d="M12 3 2 20h20L12 3z" /><path d="M12 10v4" /><path d="M12 17v.01" />
          </svg>
          Issues
          <span v-if="openIssues > 0" class="count b-bad"
            >{{ openIssues }}<span class="sr-only"> open {{ openIssues === 1 ? 'issue' : 'issues' }}</span></span
          >
        </RouterLink>
        <RouterLink :to="tabHref('/activity')" :class="['nav', on('activity'), { dim: blocked }]">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M12 8v4l3 2" /><circle cx="12" cy="12" r="9" />
          </svg>
          Activity
        </RouterLink>
        <RouterLink v-if="!readOnly" :to="remote ? tabHref('/new') : '/new'" :class="['nav', on('new')]" :aria-keyshortcuts="KEYS.newConfig.aria">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true">
            <path d="M12 5v14" /><path d="M5 12h14" />
          </svg>
          Add new
          <span class="kbd" aria-hidden="true">{{ KEYS.newConfig.hint }}</span>
        </RouterLink>
      </div>
    </div>

    <div class="section bottom">
      <RouterLink :to="remote ? tabHref('/report') : '/doctor'" :class="['nav', on('doctor')]">
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M3 12h4l3-8 4 16 3-8h4" />
        </svg>
        {{ remote ? 'Server report' : 'Doctor' }}
      </RouterLink>
      <RouterLink to="/mcp" :class="['nav', on('mcp')]">
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M8 3v4M16 3v4" /><rect x="5" y="7" width="14" height="6" rx="2" /><path d="M12 13v3a4 4 0 0 1-4 4" />
        </svg>
        MCP server
        <span v-if="mcpRunning" class="dot d-ok run" aria-hidden="true" /><span v-if="mcpRunning" class="sr-only">(running)</span>
      </RouterLink>
      <RouterLink to="/servers" :class="['nav', on('servers')]">
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <rect x="3" y="4" width="18" height="7" rx="1.5" /><rect x="3" y="13" width="18" height="7" rx="1.5" /><path d="M7 7.5h.01M7 16.5h.01" />
        </svg>
        Remote servers
      </RouterLink>
      <RouterLink to="/settings" :class="['nav', on('settings')]" :aria-keyshortcuts="KEYS.settings.aria">
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
          <circle cx="12" cy="12" r="3" />
          <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
        </svg>
        Settings
        <span class="kbd" aria-hidden="true">{{ KEYS.settings.hint }}</span>
      </RouterLink>
    </div>
  </nav>
</template>

<style scoped>
.sidebar {
  box-sizing: border-box;
  width: 232px;
  flex: none;
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 16px 12px;
  background: var(--side);
  border-right: 1px solid var(--line);
  color: var(--ink);
}
.brand { display: flex; align-items: center; gap: 9px; padding: 0 6px; }
/* The app icon's mark (ring, glass, pulse) at 22 px. */
.mark { display: inline-flex; flex: none; --mark-size: 22px; }
.wordmark {
  font-family: var(--display);
  font-optical-sizing: auto;
  font-size: 18px;
  font-weight: 650;
  letter-spacing: -0.015em;
}
.section { display: flex; flex-direction: column; gap: 2px; }
.th { padding: 0 10px 6px; font-size: 12px; font-weight: 600; color: var(--ink3); }
.server { display: flex; align-items: center; gap: 8px; height: var(--control); padding: 0 10px; }
.server-name { font-weight: 600; }
.state { margin-left: auto; height: 20px; }
.sub { display: flex; flex-direction: column; gap: 2px; padding-left: 14px; }
.nav {
  display: flex;
  align-items: center;
  gap: 8px;
  height: var(--control);
  box-sizing: border-box;
  padding: 0 10px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--ink);
  font: inherit;
  font-size: 13.5px;
  text-align: left;
  text-decoration: none;
}
.nav svg { flex: none; }
.nav:hover:not(:disabled) { background: var(--hover); }
.nav.dim:not(.on) { color: var(--ink3); }
.nav.on { background: var(--accent-soft); color: var(--accent-text); font-weight: 600; }
.nav .kbd { margin-left: auto; font-weight: 400; }
.nav .run { margin-left: auto; }
.count {
  margin-left: auto;
  min-width: 20px;
  height: 18px;
  box-sizing: border-box;
  padding: 0 6px;
  border-radius: 9px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 11.5px;
  font-weight: 700;
}
.bottom { margin-top: auto; }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
</style>
