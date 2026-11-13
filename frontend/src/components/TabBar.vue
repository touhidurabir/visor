<script setup lang="ts">
// The tabs across the top of the main window, like a browser's: this
// computer first (it can't be closed), then the servers opened this launch,
// then + to open a saved server or add one. Ctrl+Tab and Ctrl+Shift+Tab move
// between tabs; ⌘W / Ctrl+W closes a server's tab (AppShell).
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import { RemoteService, type LimitsView, type SavedServerView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { errorMessage } from '../lib/overview'
import { isMac, platform } from '../lib/platform'
import { closeServer, openServer, serverForm, switchTab } from '../lib/servers'
import { currentTab, LOCAL, serverTabs } from '../lib/tabs'
import TabItem from './TabItem.vue'

const router = useRouter()
const localName = computed(() => platform().local || 'This Mac')

const menuOpen = ref(false)
const saved = ref<SavedServerView[]>([])
const limits = ref<LimitsView | null>(null)
const loadError = ref('')
const plus = ref<HTMLButtonElement>()
const menu = ref<HTMLElement>()

const closed = computed(() => saved.value.filter((s) => !serverTabs.value.some((t) => t.id === s.Server.id)))
const full = computed(() => !!limits.value && limits.value.Open >= limits.value.MaxServers)

async function toggleMenu() {
  if (menuOpen.value) return closeMenu()
  menuOpen.value = true
  loadError.value = ''
  try {
    const [list, l] = await Promise.all([RemoteService.List(), RemoteService.Limits()])
    saved.value = list ?? []
    limits.value = l
  } catch (e) {
    loadError.value = errorMessage(e)
  }
  await nextTick()
  menu.value?.querySelector<HTMLElement>('button:not(:disabled)')?.focus()
  document.addEventListener('mousedown', onOutside)
}

function closeMenu() {
  menuOpen.value = false
  document.removeEventListener('mousedown', onOutside)
}

function onOutside(e: MouseEvent) {
  const t = e.target as Node
  if (!menu.value?.contains(t) && !plus.value?.contains(t)) closeMenu()
}
onBeforeUnmount(() => document.removeEventListener('mousedown', onOutside))

function pick(s: SavedServerView) {
  closeMenu()
  void openServer(router, s.Server)
}

function addServer() {
  closeMenu()
  serverForm.value = { server: null }
}

function onMenuKey(e: KeyboardEvent) {
  const items = Array.from(menu.value?.querySelectorAll<HTMLElement>('button:not(:disabled)') ?? [])
  const i = items.indexOf(document.activeElement as HTMLElement)
  if (e.key === 'Escape') {
    e.preventDefault()
    closeMenu()
    plus.value?.focus()
  } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    const next = items[(i + (e.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length]
    next?.focus()
  }
}

function onTabsKey(e: KeyboardEvent) {
  if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
  const ids = [LOCAL, ...serverTabs.value.map((t) => t.id)]
  const i = ids.indexOf(currentTab.value)
  const next = ids[(i + (e.key === 'ArrowRight' ? 1 : -1) + ids.length) % ids.length]
  e.preventDefault()
  switchTab(router, next)
  void nextTick(() => document.querySelector<HTMLElement>(`[data-tab="${next}"]`)?.focus())
}

function describe(s: SavedServerView): string {
  const sv = s.Server
  return `${sv.user ? sv.user + '@' : ''}${sv.host}${sv.port && sv.port !== 22 ? ':' + sv.port : ''}${sv.useSudo ? ' · Use sudo' : ''}`
}
</script>

<template>
  <div :class="['tabbar', { mac: isMac() }]">
    <div class="tabs" role="tablist" aria-label="Servers" @keydown="onTabsKey">
      <TabItem
        :id="LOCAL"
        :name="localName"
        :active="currentTab === LOCAL"
        title="This computer’s supervisord. Always open."
        @select="switchTab(router, LOCAL)"
      />
      <TabItem
        v-for="t in serverTabs"
        :id="t.id"
        :key="t.id"
        :name="t.name"
        :tag="t.tag"
        :color="t.color"
        :active="currentTab === t.id"
        :title="t.readOnly ? `${t.name} · Read only` : t.name"
        @select="switchTab(router, t.id)"
        @close="closeServer(router, t.id)"
      />
    </div>
    <div class="add">
      <button
        ref="plus"
        class="btn quiet icon plus"
        type="button"
        aria-label="Add or open a server"
        title="Open a saved server or add one"
        :aria-expanded="menuOpen"
        aria-haspopup="menu"
        @click="toggleMenu"
      >
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><path d="M12 5v14" /><path d="M5 12h14" /></svg>
      </button>
      <div v-if="menuOpen" ref="menu" class="menu" role="menu" aria-label="Servers to open" @keydown="onMenuKey">
        <p v-if="loadError" class="meta err">{{ loadError }}</p>
        <p v-else-if="full" class="meta note">
          {{ limits!.MaxServers }} servers are open, the most at once. Close one, or raise the limit in
          <RouterLink to="/settings" @click="closeMenu">Settings</RouterLink>.
        </p>
        <template v-if="closed.length">
          <span class="th">Saved servers</span>
          <button v-for="s in closed" :key="s.Server.id" class="item" type="button" role="menuitem" :disabled="full" @click="pick(s)">
            <span class="dot" :style="{ background: s.Server.color || 'var(--idle-dot)' }" aria-hidden="true" />
            <span class="what"><span class="name">{{ s.Server.name }}</span><span class="meta">{{ describe(s) }}</span></span>
          </button>
          <div class="sep" role="separator" />
        </template>
        <button class="item" type="button" role="menuitem" @click="addServer">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M12 5v14" /><path d="M5 12h14" /></svg>
          Add a server…
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tabbar {
  display: flex;
  align-items: flex-end;
  gap: 2px;
  height: 44px;
  flex: none;
  box-sizing: border-box;
  padding: 0 10px 0 16px;
  background: var(--side);
  border-bottom: 1px solid var(--line);
  --wails-draggable: drag;
}
/* Room for the macOS traffic lights (hidden-inset title bar). */
.tabbar.mac { padding-left: 84px; }
.tabs { display: flex; align-items: flex-end; gap: 2px; min-width: 0; overflow: hidden; }
.add { position: relative; align-self: center; margin-left: 4px; --wails-draggable: no-drag; }
.plus { width: 28px; height: 28px; }
.menu {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  z-index: 40;
  min-width: 280px;
  max-width: 360px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 6px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--bg);
  box-shadow: var(--shadow-lg, 0 12px 32px rgba(0, 0, 0, .18));
}
.th { padding: 4px 8px 2px; font-size: 12px; font-weight: 600; color: var(--ink3); }
.item {
  display: flex;
  align-items: center;
  gap: 9px;
  min-height: 32px;
  padding: 4px 8px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--ink);
  font: inherit;
  font-size: 13.5px;
  text-align: left;
  cursor: pointer;
}
.item:hover:not(:disabled), .item:focus-visible { background: var(--hover); outline: none; }
.item:disabled { color: var(--ink3); cursor: default; }
.what { display: flex; flex-direction: column; min-width: 0; }
.name { font-weight: 600; }
.what .meta { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sep { height: 1px; margin: 4px 2px; background: var(--line); }
.note, .err { margin: 4px 8px 6px; }
.err { color: var(--bad); }
</style>
