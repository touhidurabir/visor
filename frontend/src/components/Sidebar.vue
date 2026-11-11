<script setup lang="ts">
// The lens mark, the wordmark and the notification bell; Servers (Local with
// its health and failing count, Dashboard, Issues, Add new); Remote servers
// (later); and Doctor / Settings at the bottom. Appearance lives in Settings.
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import AppMark from './AppMark.vue'
import NotificationBell from './NotificationBell.vue'
import { isMac } from '../lib/platform'
import { shortcutAria, shortcutHint } from '../lib/shortcuts'

// `connection` is the badge for a connection problem ("Not running", "Not
// answering", "Not connected"; lib/connection.ts connectionBadge), null
// while connected.
const props = defineProps<{ name: string; health: string; failing: number; openIssues: number; connection: string | null }>()

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
  new: ['new', 'new-form', 'new-import'],
  doctor: ['doctor'],
  settings: ['settings'],
}
const on = (key: string) => ({ on: sections[key].includes(String(route.name)) })

const dot = computed(() => {
  if (props.connection) return 'd-bad'
  return { ok: 'd-ok', warning: 'd-warn', error: 'd-bad' }[props.health] ?? 'd-idle'
})
</script>

<template>
  <nav :class="['sidebar', { mac: isMac() }]" aria-label="Servers">
    <div class="brand">
      <span class="mark" aria-hidden="true"><AppMark /></span>
      <span class="wordmark">Visor</span>
      <NotificationBell />
    </div>

    <div class="section">
      <span class="th">Servers</span>
      <div class="server">
        <span :class="['dot', dot]" aria-hidden="true" />
        <span class="server-name">{{ name }}</span>
        <span class="sr-only">health {{ health }}</span>
        <span v-if="connection" class="badge b-bad state">{{ connection }}</span>
        <span v-else-if="failing > 0" class="badge b-bad state">{{ failing }} failing</span>
      </div>
      <div class="sub">
        <RouterLink to="/" :class="['nav', on('dashboard')]">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true">
            <rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" />
          </svg>
          Dashboard
        </RouterLink>
        <RouterLink to="/issues" :class="['nav', on('issues')]">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" aria-hidden="true">
            <path d="M12 3 2 20h20L12 3z" /><path d="M12 10v4" /><path d="M12 17v.01" />
          </svg>
          Issues
          <span v-if="openIssues > 0" class="count b-bad"
            >{{ openIssues }}<span class="sr-only"> open {{ openIssues === 1 ? 'issue' : 'issues' }}</span></span
          >
        </RouterLink>
        <RouterLink to="/new" :class="['nav', on('new')]" :aria-keyshortcuts="KEYS.newConfig.aria">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true">
            <path d="M12 5v14" /><path d="M5 12h14" />
          </svg>
          Add new
          <span class="kbd" aria-hidden="true">{{ KEYS.newConfig.hint }}</span>
        </RouterLink>
      </div>
    </div>

    <div class="section">
      <span class="th">Remote servers</span>
      <button type="button" class="nav disabled" disabled>
        Add a server
        <span class="badge b-idle soon">Soon</span>
      </button>
    </div>

    <div class="section bottom">
      <RouterLink to="/doctor" :class="['nav', on('doctor')]">
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M3 12h4l3-8 4 16 3-8h4" />
        </svg>
        Doctor
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
/* Top clearance for the macOS traffic lights (hidden-inset title bar); Linux
   windows have a title bar of their own. */
.sidebar.mac { padding-top: 44px; }
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
.nav.on { background: var(--accent-soft); color: var(--accent-text); font-weight: 600; }
.nav.disabled { color: var(--ink3); cursor: default; }
.nav .kbd { margin-left: auto; font-weight: 400; }
.soon { margin-left: auto; height: 18px; font-size: 11px; }
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
