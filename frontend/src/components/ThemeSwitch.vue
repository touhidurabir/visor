<script setup lang="ts">
// Appearance switch: System / Light / Dark as a radio group (arrow keys move
// and choose, one tab stop). Used in the sidebar and in Settings; both read
// the shared `appearance`, so they stay in sync.
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
  <div class="theme-switch" role="radiogroup" aria-label="Appearance">
    <button
      v-for="(a, i) in APPEARANCES"
      :key="a"
      :ref="(el) => (buttons[i] = el as HTMLButtonElement)"
      type="button"
      role="radio"
      class="opt"
      :class="{ on: appearance === a }"
      :aria-checked="appearance === a"
      :aria-label="labels[a]"
      :title="labels[a]"
      :tabindex="appearance === a ? 0 : -1"
      :data-appearance="a"
      @click="choose(a)"
      @keydown="onKey($event, i)"
    >
      <svg v-if="a === 'system'" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <rect x="3" y="4" width="18" height="12" rx="2" /><path d="M8 20h8M12 16v4" />
      </svg>
      <svg v-else-if="a === 'light'" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
      </svg>
      <svg v-else width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
      </svg>
    </button>
  </div>
</template>

<style scoped>
.theme-switch {
  display: inline-flex;
  gap: 2px;
  padding: 2px;
  border-radius: var(--radius-sm);
  background: var(--seg-bg);
}
.opt {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 24px;
  padding: 0;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}
.opt:hover { color: var(--text); }
.opt.on { background: var(--seg-on); color: var(--text); box-shadow: 0 1px 2px rgba(0, 0, 0, 0.18); }
</style>
