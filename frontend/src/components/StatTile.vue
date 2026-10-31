<script setup lang="ts">
// One overview tile. With `to` it is a link (Open issues -> Issues page).
import { RouterLink } from 'vue-router'

defineProps<{ label: string; value: number; hint: string; tone?: 'running' | 'failing'; to?: string }>()
</script>

<template>
  <component :is="to ? RouterLink : 'div'" :to="to" :class="['tile', tone, { alert: tone === 'failing' && value > 0 }]">
    <span class="lbl">{{ label }}</span>
    <b>{{ value }}</b>
    <span class="hint">{{ hint }}</span>
  </component>
</template>

<style scoped>
.tile {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 14px 16px;
  border: 1px solid var(--line);
  border-radius: 10px;
  color: var(--text);
  text-decoration: none;
}
a.tile:hover { background: var(--bg-subtle); }
.lbl { color: var(--text-muted); }
b { font-size: 26px; font-weight: 650; line-height: 1.1; }
.hint { color: var(--text-muted); font-size: 12px; }
.running .lbl { color: var(--running-fg); }
.failing .lbl { color: var(--failing-fg); }
.alert { border-color: var(--failing-dot); background: var(--failing-bg); }
</style>
