<script setup lang="ts">
// Renders the toast queue from lib/toast.ts. Errors use role="alert" so a
// screen reader announces them; info toasts are polite.
import { dismissToast, toasts } from '../lib/toast'
</script>

<template>
  <div class="toasts">
    <div v-for="t in toasts" :key="t.id" :class="['toast', t.kind]" :role="t.kind === 'error' ? 'alert' : 'status'">
      <span>{{ t.text }}</span>
      <button type="button" aria-label="Dismiss message" @click="dismissToast(t.id)">×</button>
    </div>
  </div>
</template>

<style scoped>
.toasts { position: fixed; right: 20px; bottom: 20px; z-index: 30; display: flex; flex-direction: column; gap: 8px; width: min(380px, calc(100vw - 40px)); }
.toast {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--radius-md);
  border: 1px solid var(--line-strong);
  background: var(--menu-bg);
  color: var(--text);
  box-shadow: var(--menu-shadow);
  line-height: 1.4;
  user-select: text;
}
.toast.error { background: var(--failing-bg); color: var(--failing-fg); border-color: var(--failing-dot); }
.toast span { flex: 1; }
.toast button { border: 0; background: transparent; color: inherit; font-size: 18px; line-height: 1; cursor: pointer; padding: 0 2px; }
</style>
