<script setup lang="ts">
// The main window frame: the tab bar across the top, then the sidebar and a
// scrolling content area for the tab on screen. The sidebar health comes
// from the same visor:overview feed as every other view.
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { connectionBadge, connectionProblem } from '../lib/connection'
import { placeName, useOverview } from '../lib/overview'
import { Application, Window } from '@wailsio/runtime'
import { followMenu, onMenu, runMenu } from '../lib/menu'
import { followDrops } from '../lib/import-files'
import { isMac } from '../lib/platform'
import { shortcutOf } from '../lib/shortcuts'
import { closeServer, hostKeyPrompt, refreshTabs, serverForm, switchTab } from '../lib/servers'
import { currentTab, LOCAL, neighbour, rememberPath } from '../lib/tabs'
import HostKeyDialog from './HostKeyDialog.vue'
import McpAsk from './McpAsk.vue'
import ProductionPrompt from './ProductionPrompt.vue'
import ServerForm from './ServerForm.vue'
import Sidebar from './Sidebar.vue'
import TabBar from './TabBar.vue'
import ToastHost from './ToastHost.vue'

const router = useRouter()
const route = useRoute()
const { overview } = useOverview()

// Open tabs survive a reload of the page; each tab remembers its page.
void refreshTabs()
watch(
  () => route.fullPath,
  (p) => {
    if (!route.meta.bare && route.name !== 'settings') rememberPath(currentTab.value, p)
  },
  { immediate: true },
)

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
  // Ctrl+Tab and Ctrl+Shift+Tab move between tabs, as in a browser.
  if (!e.defaultPrevented && e.key === 'Tab' && e.ctrlKey && !e.metaKey && !e.altKey) {
    e.preventDefault()
    return switchTab(router, neighbour(e.shiftKey ? -1 : 1))
  }
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
    case 'closeWindow': return closeTabOrWindow()
    case 'quit': return void Application.Quit()
  }
}
// ⌘W / Ctrl+W closes a server's tab; on this computer's tab, the window.
function closeTabOrWindow() {
  if (currentTab.value !== LOCAL) return void closeServer(router, currentTab.value)
  void Window.Close()
}
onMenu('close', closeTabOrWindow)

onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
const name = computed(() => placeName(overview.value))
const remote = computed(() => currentTab.value !== LOCAL)
const color = computed(() => overview.value?.Host?.Color ?? '')
const blocked = computed(() => remote.value && !!overview.value && !overview.value.Server.Reachable && overview.value.Server.Health !== 'unknown')
const health = computed(() => overview.value?.Server.Health ?? 'unknown')
const failing = computed(() => overview.value?.Stats.Failing ?? 0)
const openIssues = computed(() => overview.value?.Stats.OpenIssues ?? 0)
// "Not running", "Not answering" or "Not connected" beside the server's
// name, as the dashboard and popover word the problem; null while connected.
const connection = computed(() => connectionBadge(connectionProblem(overview.value?.Server)))
</script>

<template>
  <div class="frame">
    <TabBar />
    <div class="shell" data-file-drop-target>
      <Sidebar :name="name" :health="health" :failing="failing" :open-issues="openIssues" :connection="connection" :remote="remote" :read-only="!!overview?.Host?.ReadOnly" :color="color" :blocked="blocked" />
      <main class="content"><slot /></main>
    </div>
    <ServerForm v-if="serverForm" :key="`${serverForm.server?.Server.id}-${serverForm.report?.AtMs ?? 0}`" :editing="serverForm.server" :report="serverForm.report" @close="serverForm = null" />
    <ProductionPrompt />
    <McpAsk />
    <HostKeyDialog v-if="hostKeyPrompt" :view="hostKeyPrompt.view" :server="hostKeyPrompt.server" @trusted="hostKeyPrompt?.then()" @close="hostKeyPrompt = null" />
    <ToastHost />
  </div>
</template>

<style scoped>
.frame { display: flex; flex-direction: column; height: 100vh; background: var(--bg); color: var(--text); }
.shell { display: flex; flex: 1; min-height: 0; background: var(--bg); color: var(--text); }
/* Files dragged over the window (they open in Add new → Import files). */
.shell.file-drop-target-active .content { outline: 2px dashed var(--accent); outline-offset: -6px; }
.content { flex: 1; min-width: 0; overflow: auto; box-sizing: border-box; padding: 40px 28px 40px; }
</style>
