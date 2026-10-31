<script setup lang="ts">
// A "..." trigger with a menu of MenuItems (lib/menus.ts). Keyboard: Enter,
// Space or Down opens; Up/Down/Home/End move; Enter activates; Esc or Tab
// closes. Focus returns to the trigger. Items with `confirm` ask first, and
// the select event fires only after the person confirms.
import { nextTick, onBeforeUnmount, ref } from 'vue'
import type { MenuItem } from '../lib/menus'
import ConfirmDialog from './ConfirmDialog.vue'

const props = defineProps<{ items: MenuItem[]; label: string; heading?: string }>()
const emit = defineEmits<{ select: [id: string] }>()

const open = ref(false)
const pending = ref<MenuItem | null>(null)
const root = ref<HTMLElement>()
const trigger = ref<HTMLButtonElement>()
const menu = ref<HTMLElement>()

const enabledButtons = () => Array.from(menu.value?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])

function onOutside(e: MouseEvent) {
  if (!root.value?.contains(e.target as Node)) close(false)
}

async function show(focus: 'first' | 'last' | 'none' = 'first') {
  open.value = true
  document.addEventListener('mousedown', onOutside)
  await nextTick()
  const btns = enabledButtons()
  if (focus === 'first') btns[0]?.focus()
  else if (focus === 'last') btns[btns.length - 1]?.focus()
  else menu.value?.focus()
}

function close(returnFocus = true) {
  open.value = false
  document.removeEventListener('mousedown', onOutside)
  if (returnFocus) trigger.value?.focus()
}

onBeforeUnmount(() => document.removeEventListener('mousedown', onOutside))

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
    <div v-if="open" ref="menu" class="menu" role="menu" :aria-label="props.label" tabindex="-1" @keydown="onMenuKey">
      <div v-if="props.heading" class="heading" aria-hidden="true">{{ props.heading }}</div>
      <button
        v-for="item in props.items"
        :key="item.id"
        type="button"
        role="menuitem"
        :class="['mi', { danger: item.danger }]"
        :disabled="item.disabled"
        @click="choose(item)"
      >
        <span>{{ item.label }}</span>
        <span v-if="item.cmd" class="cmd">{{ item.cmd }}</span>
      </button>
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
  position: absolute;
  right: 0;
  top: 36px;
  z-index: 5;
  min-width: 280px;
  width: max-content;
  max-width: 420px;
  display: flex;
  flex-direction: column;
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
.mi {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  min-height: 30px;
  padding: 0 10px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.cmd { font-family: var(--font-mono); font-size: 12px; color: var(--text-faint); white-space: nowrap; }
.mi:hover:not(:disabled), .mi:focus-visible:not(:disabled) {
  background: var(--accent);
  color: var(--on-accent);
  outline: none;
}
.mi:hover:not(:disabled) .cmd, .mi:focus-visible:not(:disabled) .cmd { color: var(--menu-cmd-hover); }
.mi:disabled { color: var(--text-faint); cursor: default; opacity: 0.8; }
</style>
