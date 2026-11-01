<script setup lang="ts">
// The frame of a modal with content of its own (diff previews, backups):
// scrim, title, a scrolling body, a footer slot. Focus moves into the
// dialog and Tab stays inside it; Esc or the backdrop closes it. For plain
// yes/no questions use ConfirmDialog.
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

const props = defineProps<{ title: string; wide?: boolean }>()
const emit = defineEmits<{ close: [] }>()

const dialog = ref<HTMLElement>()
const titleId = `dialog-title-${Math.random().toString(36).slice(2)}`
let opener: Element | null = null

const FOCUSABLE = 'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'

onMounted(() => {
  opener = document.activeElement
  void nextTick(() => {
    const first = dialog.value?.querySelector<HTMLElement>('[data-autofocus]') ?? dialog.value?.querySelector<HTMLElement>(FOCUSABLE)
    ;(first ?? dialog.value)?.focus()
  })
})
onBeforeUnmount(() => {
  if (opener instanceof HTMLElement) opener.focus()
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    emit('close')
  } else if (e.key === 'Tab' && dialog.value) {
    const items = Array.from(dialog.value.querySelectorAll<HTMLElement>(FOCUSABLE))
    if (!items.length) return
    const first = items[0]
    const last = items[items.length - 1]
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault()
      first.focus()
    }
  }
}
</script>

<template>
  <Teleport to="body">
    <div class="scrim" @click.self="emit('close')" @keydown="onKeydown">
      <div ref="dialog" :class="['dialog', { wide: props.wide }]" role="dialog" aria-modal="true" :aria-labelledby="titleId" tabindex="-1">
        <h2 :id="titleId">{{ props.title }}</h2>
        <div class="body"><slot /></div>
        <div class="actions"><slot name="actions" /></div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.scrim { position: fixed; inset: 0; z-index: 20; display: flex; align-items: center; justify-content: center; background: var(--scrim); }
.dialog {
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: min(520px, calc(100vw - 48px));
  max-height: calc(100vh - 64px);
  padding: 20px;
  box-sizing: border-box;
  border-radius: var(--radius-lg);
  background: var(--menu-bg);
  color: var(--text);
  border: 1px solid var(--line-strong);
  box-shadow: var(--menu-shadow);
  user-select: text;
}
.dialog.wide { width: min(820px, calc(100vw - 48px)); }
.dialog:focus { outline: none; }
h2 { margin: 0; font-size: 15px; font-weight: 650; }
.body { min-height: 0; overflow: auto; display: flex; flex-direction: column; gap: 10px; }
.actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; }
</style>
