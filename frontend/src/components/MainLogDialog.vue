<script setup lang="ts">
// The last 64 KiB of supervisord's own log. Esc or the backdrop closes it.
import { nextTick, onMounted, ref } from 'vue'
import { ServerService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { describeError } from '../lib/faults'

const emit = defineEmits<{ close: [] }>()
const text = ref<string | null>(null)
const error = ref<string | null>(null)
const pre = ref<HTMLElement>()
const closeBtn = ref<HTMLButtonElement>()

onMounted(async () => {
  void nextTick(() => closeBtn.value?.focus())
  try {
    text.value = await ServerService.MainLog(65536)
    await nextTick()
    if (pre.value) pre.value.scrollTop = pre.value.scrollHeight
  } catch (e) {
    error.value = describeError(e)
  }
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    emit('close')
  }
}
</script>

<template>
  <Teleport to="body">
    <div class="scrim" @click.self="emit('close')" @keydown="onKeydown">
      <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="log-title">
        <h2 id="log-title">supervisord log</h2>
        <p v-if="error" class="err" role="alert">Couldn’t read the log. {{ error }}</p>
        <p v-else-if="text === null" class="muted">Loading…</p>
        <p v-else-if="!text.trim()" class="muted">The log is empty.</p>
        <pre v-else ref="pre" tabindex="0" aria-label="supervisord log text">{{ text }}</pre>
        <div class="actions"><button ref="closeBtn" type="button" class="btn" @click="emit('close')">Close</button></div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.scrim { position: fixed; inset: 0; z-index: 20; display: flex; align-items: center; justify-content: center; background: var(--scrim); }
.dialog {
  display: flex;
  flex-direction: column;
  width: min(820px, calc(100vw - 48px));
  max-height: calc(100vh - 80px);
  padding: 20px;
  border-radius: var(--radius-lg);
  background: var(--menu-bg);
  color: var(--text);
  border: 1px solid var(--line-strong);
  box-shadow: var(--menu-shadow);
}
h2 { margin: 0 0 12px; font-size: 15px; font-weight: 650; }
pre {
  flex: 1;
  min-height: 160px;
  margin: 0 0 16px;
  padding: 10px 12px;
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  background: var(--bg);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
  user-select: text;
}
.muted { color: var(--text-muted); }
.err { color: var(--failing-fg); }
.actions { display: flex; justify-content: flex-end; }
.btn { min-height: 32px; padding: 0 14px; border-radius: var(--radius-sm); border: 1px solid var(--line-strong); background: var(--bg); color: var(--text); font: inherit; cursor: pointer; }
.btn:hover { background: var(--bg-hover); }
</style>
