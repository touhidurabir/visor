<script setup lang="ts">
// A small modal for confirming actions, destructive ones by default (a red
// confirm button; pass :danger="false" for one that is not). Cancel has
// initial focus, Tab stays inside the dialog, Esc or the backdrop cancels.
import { nextTick, onMounted, ref } from 'vue'

const props = withDefaults(defineProps<{ title: string; message: string; confirmLabel: string; danger?: boolean }>(), { danger: true })
const emit = defineEmits<{ confirm: []; cancel: [] }>()

const cancelBtn = ref<HTMLButtonElement>()
const confirmBtn = ref<HTMLButtonElement>()
const titleId = `confirm-title-${Math.random().toString(36).slice(2)}`

onMounted(() => void nextTick(() => cancelBtn.value?.focus()))

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    emit('cancel')
  } else if (e.key === 'Tab') {
    const first = cancelBtn.value
    const last = confirmBtn.value
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault()
      last?.focus()
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault()
      first?.focus()
    }
  }
}
</script>

<template>
  <Teleport to="body">
    <div class="scrim" @click.self="emit('cancel')" @keydown="onKeydown">
      <div class="dialog" role="alertdialog" aria-modal="true" :aria-labelledby="titleId">
        <h2 :id="titleId">{{ props.title }}</h2>
        <p>{{ props.message }}</p>
        <div class="actions">
          <button ref="cancelBtn" type="button" class="btn" @click="emit('cancel')">Cancel</button>
          <button ref="confirmBtn" type="button" :class="['btn', 'primary', { danger: props.danger }]" @click="emit('confirm')">
            {{ props.confirmLabel }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.scrim {
  position: fixed;
  inset: 0;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--scrim);
}
.dialog {
  width: min(420px, calc(100vw - 48px));
  padding: 20px;
  border-radius: var(--radius-lg);
  background: var(--menu-bg);
  color: var(--text);
  border: 1px solid var(--line-strong);
  box-shadow: var(--menu-shadow);
  user-select: text;
}
h2 { margin: 0 0 8px; font-size: 15px; font-weight: 650; }
p { margin: 0 0 18px; color: var(--text-muted); line-height: 1.45; white-space: pre-line; }
.actions { display: flex; justify-content: flex-end; gap: 8px; }
</style>
