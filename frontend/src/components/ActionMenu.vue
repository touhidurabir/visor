<script setup lang="ts">
// A "..." trigger with a menu of MenuItems (lib/menus.ts). Keyboard: Enter,
// Space or Down opens; Up/Down/Home/End move; Enter activates; Esc or Tab
// closes. Focus returns to the trigger. Items with `confirm` ask first, and
// the select event fires only after the person confirms. Each item with a
// command has a copy button (mouse; not in the arrow-key cycle or Tab order)
// and Cmd/Ctrl+C copies the command of the focused item. The menu is fixed
// to the viewport, so a scrolling table never clips it, and flips upward or
// leftward when it would run off the window.
import { nextTick, onBeforeUnmount, ref } from 'vue'
import { copyText } from '../lib/clipboard'
import { menuIcons } from '../lib/menu-icons'
import type { MenuItem } from '../lib/menus'
import { showToast } from '../lib/toast'
import ConfirmDialog from './ConfirmDialog.vue'

const props = defineProps<{ items: MenuItem[]; label: string; heading?: string }>()
const emit = defineEmits<{ select: [id: string] }>()

const open = ref(false)
const pending = ref<MenuItem | null>(null)
const root = ref<HTMLElement>()
const trigger = ref<HTMLButtonElement>()
const menu = ref<HTMLElement>()

const pos = ref<{ top?: string; bottom?: string; left?: string; right?: string; maxHeight: string }>({ maxHeight: '' })
const MENU_W = 360
const GAP = 4
const EDGE = 8

const enabledButtons = () => Array.from(menu.value?.querySelectorAll<HTMLButtonElement>('button.mi:not(:disabled)') ?? [])

/** Places the open menu under (or over) the trigger, inside the viewport. */
function place() {
  const t = trigger.value?.getBoundingClientRect()
  const m = menu.value
  if (!t || !m) return
  const h = m.scrollHeight + 2
  const below = window.innerHeight - t.bottom - GAP - EDGE
  const above = t.top - GAP - EDGE
  const up = h > below && above > below
  const room = up ? above : below
  // Prefer growing leftward from the trigger's right edge.
  const left = t.right - MENU_W >= EDGE ? t.right - MENU_W : Math.min(t.left, window.innerWidth - MENU_W - EDGE)
  pos.value = {
    ...(up ? { bottom: `${window.innerHeight - t.top + GAP}px` } : { top: `${t.bottom + GAP}px` }),
    left: `${Math.max(EDGE, left)}px`,
    maxHeight: `${Math.max(120, room)}px`,
  }
}

function onViewport(e: Event) {
  if (e.type === 'scroll' && menu.value?.contains(e.target as Node)) return
  close(false)
}

function onOutside(e: MouseEvent) {
  if (!root.value?.contains(e.target as Node)) close(false)
}

async function show(focus: 'first' | 'last' | 'none' = 'first') {
  open.value = true
  document.addEventListener('mousedown', onOutside)
  window.addEventListener('resize', onViewport)
  window.addEventListener('scroll', onViewport, true)
  await nextTick()
  place()
  const btns = enabledButtons()
  if (focus === 'first') btns[0]?.focus()
  else if (focus === 'last') btns[btns.length - 1]?.focus()
  else menu.value?.focus()
}

function close(returnFocus = true) {
  open.value = false
  document.removeEventListener('mousedown', onOutside)
  window.removeEventListener('resize', onViewport)
  window.removeEventListener('scroll', onViewport, true)
  if (returnFocus) trigger.value?.focus()
}

onBeforeUnmount(() => close(false))

function toggle(e: MouseEvent) {
  if (open.value) close()
  else void show(e.detail === 0 ? 'first' : 'none') // keyboard-initiated clicks have detail 0
}

function onTriggerKey(e: KeyboardEvent) {
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    void show(e.key === 'ArrowDown' ? 'first' : 'last')
  }
}

function onMenuKey(e: KeyboardEvent) {
  const btns = enabledButtons()
  const i = btns.indexOf(document.activeElement as HTMLButtonElement)
  switch (e.key) {
    case 'ArrowDown':
      e.preventDefault()
      btns[(i + 1) % btns.length]?.focus()
      break
    case 'ArrowUp':
      e.preventDefault()
      btns[(i <= 0 ? btns.length : i) - 1]?.focus()
      break
    case 'Home':
      e.preventDefault()
      btns[0]?.focus()
      break
    case 'End':
      e.preventDefault()
      btns[btns.length - 1]?.focus()
      break
    case 'c':
    case 'C': {
      const id = (document.activeElement as HTMLElement | null)?.dataset.id
      const item = props.items.find((it) => it.id === id)
      if ((e.metaKey || e.ctrlKey) && item?.cmd) {
        e.preventDefault()
        void copyCommand(item)
      }
      break
    }
    case 'Escape':
      e.preventDefault()
      e.stopPropagation()
      close()
      break
    case 'Tab':
      close(false)
      break
  }
}

async function copyCommand(item: MenuItem) {
  close()
  try {
    await copyText(item.cmd)
    showToast('Copied')
  } catch {
    showToast('Can’t copy: the clipboard is not available.', 'error')
  }
}

function choose(item: MenuItem) {
  if (item.disabled) return
  close()
  if (item.confirm) pending.value = item
  else emit('select', item.id)
}

function confirmed() {
  const item = pending.value
  pending.value = null
  trigger.value?.focus()
  if (item) emit('select', item.id)
}

function cancelled() {
  pending.value = null
  trigger.value?.focus()
}
</script>

<template>
  <div ref="root" class="action-menu">
    <button
      ref="trigger"
      type="button"
      class="trigger"
      aria-haspopup="menu"
      :aria-expanded="open"
      :aria-label="props.label"
      :title="props.label"
      @click="toggle"
      @keydown="onTriggerKey"
    >
      <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <circle cx="5" cy="12" r="1.8" /><circle cx="12" cy="12" r="1.8" /><circle cx="19" cy="12" r="1.8" />
      </svg>
    </button>
    <div v-if="open" ref="menu" class="menu" role="menu" :aria-label="props.label" tabindex="-1" :style="pos" @keydown="onMenuKey">
      <div v-if="props.heading" class="heading" aria-hidden="true">{{ props.heading }}</div>
      <div v-for="item in props.items" :key="item.id" :class="['item', { off: item.disabled, danger: item.danger }]" role="none">
        <button
          type="button"
          role="menuitem"
          class="mi"
          :data-id="item.id"
          :disabled="item.disabled"
          :title="item.title"
          @click="choose(item)"
        >
          <svg v-if="menuIcons[item.id]" class="ico" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="menuIcons[item.id]" /></svg>
          <span class="txt">
            <span class="lbl">{{ item.label }}</span>
            <span v-if="item.cmd" class="cmd" :title="item.cmd">{{ item.cmd }}</span>
          </span>
        </button>
        <button
          v-if="item.cmd && !item.disabled"
          type="button"
          class="copy"
          tabindex="-1"
          :aria-label="`Copy command: ${item.cmd}`"
          title="Copy command (⌘C)"
          @click="copyCommand(item)"
        >
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 9h11v11H9zM5 15V4h11" /></svg>
        </button>
      </div>
    </div>
    <ConfirmDialog
      v-if="pending"
      :title="pending.label"
      :message="pending.confirm ?? ''"
      :confirm-label="pending.label"
      @confirm="confirmed"
      @cancel="cancelled"
    />
  </div>
</template>

<style scoped>
.action-menu { position: relative; display: inline-block; }
.trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--stopped-fg);
  cursor: pointer;
}
.trigger:hover, .trigger[aria-expanded='true'] { background: var(--bg-hover); }
.menu {
  position: fixed;
  z-index: 20;
  width: 360px;
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  padding: 5px;
  background: var(--menu-bg);
  border: 1px solid var(--line-strong);
  border-radius: var(--radius-md);
  box-shadow: var(--menu-shadow);
  outline: none;
}
.heading {
  padding: 6px 10px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--text-muted);
}
.item { display: flex; align-items: center; border-radius: 6px; color: var(--text); }
.item.danger { color: var(--failing-fg); }
.item.off { color: var(--text-faint); }
.mi {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 32px;
  box-sizing: border-box;
  padding: 4px 10px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: inherit;
  font: inherit;
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}
.mi:disabled { cursor: default; opacity: 0.8; }
.ico { flex: none; }
.txt { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
.lbl { white-space: nowrap; }
.cmd { font-family: var(--font-mono); font-size: 11px; color: var(--text-faint); overflow-wrap: anywhere; line-height: 1.35; }
.copy {
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  margin-right: 6px;
  padding: 0;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--text-faint);
  cursor: pointer;
}
.copy:hover { background: var(--bg-hover); color: var(--text); }
.item:not(.off):hover, .item:not(.off):focus-within { background: var(--accent); color: var(--on-accent); }
.item:not(.off):hover .cmd, .item:not(.off):focus-within .cmd { color: var(--menu-cmd-hover); }
.item:not(.off):hover .copy, .item:not(.off):focus-within .copy { color: var(--menu-cmd-hover); }
.item:not(.off):hover .copy:hover { background: rgba(255, 255, 255, 0.2); color: var(--on-accent); }
.mi:focus-visible { outline: none; }
</style>
