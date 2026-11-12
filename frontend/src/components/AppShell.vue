<script setup lang="ts">
// The main window frame: sidebar plus a scrolling content area. The sidebar
// health comes from the same visor:overview feed as every other view.
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { connectionBadge, connectionProblem } from '../lib/connection'
import { useOverview } from '../lib/overview'
import { Application, Window } from '@wailsio/runtime'
import { followMenu, runMenu } from '../lib/menu'
import { followDrops } from '../lib/import-files'
import { isMac } from '../lib/platform'
import { shortcutOf } from '../lib/shortcuts'
import Sidebar from './Sidebar.vue'
import ToastHost from './ToastHost.vue'

const router = useRouter()
const { overview } = useOverview()

// Choices from Visor's menu bar (Find, Reload Config, Restart All…).
followMenu(router)
// Config files dropped anywhere on the window open in Add new → Import files.
followDrops(router)

// Visor's shortcuts (lib/shortcuts.ts). On macOS the menu bar takes its
// keys before the page sees them, so these run in the browser build and for
// ⌘N, which the page takes first so it runs once. Linux has no menu bar, so
// every shortcut is carried out here, Close Window and Quit included. A key
// an editor already handled (CodeMirror's own Ctrl+F) is left alone.
function onKey(e: KeyboardEvent) {
  const id = e.defaultPrevented ? null : shortcutOf(e)
  if (!id) return
  const nativeOnly = id === 'closeWindow' || id === 'quit'
  if (nativeOnly && isMac()) return
  e.preventDefault() // never the webview's own reload or find
  switch (id) {
    case 'newConfig': return void router.push('/new')
    case 'settings': return void router.push('/settings')
    case 'dashboard': return void router.push('/')
    case 'issues': return void router.push('/issues')
    case 'find': return runMenu('find')
    case 'reload': return runMenu('reload')
    case 'closeWindow': return void Window.Close()
    case 'quit': return void Application.Quit()
  }
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
const name = computed(() => overview.value?.Server.Name ?? 'Local')
const health = computed(() => overview.value?.Server.Health ?? 'unknown')
const failing = computed(() => overview.value?.Stats.Failing ?? 0)
const openIssues = computed(() => overview.value?.Stats.OpenIssues ?? 0)
// "Not running", "Not answering" or "Not connected" beside the server's
// name, as the dashboard and popover word the problem; null while connected.
const connection = computed(() => connectionBadge(connectionProblem(overview.value?.Server)))
</script>

<template>
  <div class="shell" data-file-drop-target>
    <Sidebar :name="name" :health="health" :failing="failing" :open-issues="openIssues" :connection="connection" />
    <main class="content"><slot /></main>
    <ToastHost />
  </div>
</template>

<style scoped>
.shell { display: flex; height: 100vh; background: var(--bg); color: var(--text); }
/* Files dragged over the window (they open in Add new → Import files). */
.shell.file-drop-target-active .content { outline: 2px dashed var(--accent); outline-offset: -6px; }
.content { flex: 1; min-width: 0; overflow: auto; box-sizing: border-box; padding: 40px 28px 40px; }
</style>
