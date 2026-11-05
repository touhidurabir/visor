<script setup lang="ts">
// Appearance switch: System / Light / Dark as a radio group (arrow keys move
// and choose, one tab stop). Used in Settings; it reads the shared
// `appearance`, which the tray popover follows too.
import { ref } from 'vue'
import { describeError } from '../lib/faults'
import { APPEARANCES, type Appearance } from '../lib/theme'
import { appearance, setAppearance } from '../lib/theme-sync'
import { showToast } from '../lib/toast'

const labels: Record<Appearance, string> = { system: 'System', light: 'Light', dark: 'Dark' }
const buttons = ref<HTMLButtonElement[]>([])

async function choose(a: Appearance) {
  try {
    await setAppearance(a)
  } catch (e) {
    showToast(`Couldn’t save the appearance: ${describeError(e)}`, 'error')
  }
}

function onKey(e: KeyboardEvent, i: number) {
  const step = e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : 0
  if (!step) return
  e.preventDefault()
  const j = (i + step + APPEARANCES.length) % APPEARANCES.length
  buttons.value[j]?.focus()
  void choose(APPEARANCES[j])
}
</script>

<template>
  <!-- The shared segmented control (board 12), with the words on the buttons. -->
  <div class="seg" role="radiogroup" aria-label="Appearance">
    <button
      v-for="(a, i) in APPEARANCES"
      :key="a"
      :ref="(el) => (buttons[i] = el as HTMLButtonElement)"
      type="button"
      role="radio"
      :class="{ on: appearance === a }"
      :aria-checked="appearance === a"
      :tabindex="appearance === a ? 0 : -1"
      :data-appearance="a"
      @click="choose(a)"
      @keydown="onKey($event, i)"
    >
      {{ labels[a] }}
    </button>
  </div>
</template>
