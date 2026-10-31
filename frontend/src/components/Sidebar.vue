<script setup lang="ts">
// Servers (Local with its health and failing count, Dashboard, Issues, Add
// new), Remote (later), and Doctor / Settings at the bottom.
import { RouterLink, useRoute } from 'vue-router'
import ThemeSwitch from './ThemeSwitch.vue'

defineProps<{ name: string; health: string; failing: number; openIssues: number }>()

// A config's detail page belongs under Dashboard, as in the wireframe.
const route = useRoute()
</script>

<template>
  <nav class="sidebar" aria-label="Servers">
    <div class="brand">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M2 12s3.5-6 10-6 10 6 10 6-3.5 6-10 6S2 12 2 12z" /><circle cx="12" cy="12" r="2.5" />
      </svg>
      Visor
    </div>

    <div class="label">Servers</div>
    <div class="server">
      <span :class="['dot', 'health-' + health]" aria-hidden="true" />
      {{ name }}
      <span class="sr-only">health {{ health }}</span>
      <span v-if="failing > 0" class="failing">{{ failing }} failing</span>
    </div>
    <div class="sub">
      <RouterLink to="/" :class="['nav', { current: route.name === 'config' }]">Dashboard</RouterLink>
      <RouterLink to="/issues" class="nav">
        Issues
        <span v-if="openIssues > 0" class="count"
          >{{ openIssues }}<span class="sr-only"> open {{ openIssues === 1 ? 'issue' : 'issues' }}</span></span
        >
      </RouterLink>
      <RouterLink to="/new" class="nav">Add new</RouterLink>
    </div>

    <div class="label remote">Remote</div>
    <button type="button" class="nav disabled" disabled>+ Add server<span class="later">later</span></button>

    <div class="bottom">
      <div class="appearance"><span class="label">Appearance</span><ThemeSwitch /></div>
      <RouterLink to="/doctor" class="nav">Doctor</RouterLink>
      <RouterLink to="/settings" class="nav">Settings</RouterLink>
    </div>
  </nav>
</template>

<style scoped>
.sidebar {
  box-sizing: border-box;
  width: 220px;
  flex: none;
  display: flex;
  flex-direction: column;
  gap: 4px;
  /* Top clearance for the macOS traffic lights (hidden-inset title bar). */
  padding: 40px 10px 16px;
  background: var(--bg-sidebar);
  border-right: 1px solid var(--line);
}
.brand { display: flex; align-items: center; gap: 8px; padding: 4px 10px 14px; font-weight: 650; font-size: 15px; }
.label { padding: 0 10px 4px; font-size: 11px; font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; color: var(--text-muted); }
.label.remote { padding-top: 18px; }
.server { display: flex; align-items: center; gap: 8px; min-height: 30px; padding: 0 10px; font-weight: 600; }
.dot { width: 8px; height: 8px; border-radius: 50%; flex: none; background: var(--stopped-dot); }
.dot.health-ok { background: var(--running-dot); }
.dot.health-warning { background: var(--warning-dot); }
.dot.health-error { background: var(--failing-dot); }
.failing { margin-left: auto; color: var(--failing-fg); font-size: 12px; font-weight: 600; }
.sub { display: flex; flex-direction: column; gap: 2px; padding-left: 16px; }
.nav {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 30px;
  padding: 0 10px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text);
  font: inherit;
  text-align: left;
  text-decoration: none;
}
.nav:hover:not(:disabled) { background: var(--bg-hover); }
.nav.router-link-exact-active, .nav.current { background: var(--bg-active); font-weight: 600; }
.nav.disabled { color: var(--text-faint); }
.later { margin-left: auto; font-size: 11px; }
.count {
  margin-left: auto;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--failing-bg);
  color: var(--failing-fg);
  font-size: 12px;
  font-weight: 600;
}
.bottom { margin-top: auto; display: flex; flex-direction: column; gap: 2px; padding-top: 24px; }
.appearance { display: flex; align-items: center; justify-content: space-between; padding: 0 0 8px 10px; }
.appearance .label { padding: 0; text-transform: none; letter-spacing: 0; font-size: 12px; font-weight: 500; }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
</style>
