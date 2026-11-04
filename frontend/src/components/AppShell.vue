<script setup lang="ts">
// The main window frame: sidebar plus a scrolling content area. The sidebar
// health comes from the same visor:overview feed as every other view.
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useOverview } from '../lib/overview'
import { followMenu } from '../lib/menu'
import Sidebar from './Sidebar.vue'
import ToastHost from './ToastHost.vue'

const router = useRouter()
const { overview } = useOverview()

// Choices from Visor's menu bar (Find, Reload Config, Restart All…).
followMenu(router)

// ⌘N adds a new config (spec §4.5). In the app the menu bar has it too;
// this handler takes the key first (and stops it), so it runs once, and it
// keeps the shortcut in the browser build.
function onKey(e: KeyboardEvent) {
  if (e.metaKey && !e.altKey && !e.ctrlKey && !e.shiftKey && e.key === 'n') {
    e.preventDefault()
    void router.push('/new')
  }
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
const name = computed(() => overview.value?.Server.Name ?? 'Local')
const health = computed(() => overview.value?.Server.Health ?? 'unknown')
const failing = computed(() => overview.value?.Stats.Failing ?? 0)
const openIssues = computed(() => overview.value?.Stats.OpenIssues ?? 0)
// supervisord isn't running (or can't be reached), once the first poll has said so.
const down = computed(() => {
  const server = overview.value?.Server
  return !!server && server.Health !== 'unknown' && !server.Reachable
})
</script>

<template>
  <div class="shell">
    <Sidebar :name="name" :health="health" :failing="failing" :open-issues="openIssues" :down="down" />
    <main class="content"><slot /></main>
    <ToastHost />
  </div>
</template>

<style scoped>
.shell { display: flex; height: 100vh; background: var(--bg); color: var(--text); }
.content { flex: 1; min-width: 0; overflow: auto; box-sizing: border-box; padding: 40px 28px 40px; }
</style>
