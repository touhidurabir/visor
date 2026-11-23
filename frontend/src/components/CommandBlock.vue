<script setup lang="ts">
// A command or config lines to copy, in the log colours, with a Copy button
// (as Doctor shows its fixes). Without `copy` it is just the text. `shown`,
// when set, is displayed instead of text (a token hidden), and text is copied.
import { ref } from 'vue'
import { copyText } from '../lib/clipboard'
import { errorMessage } from '../lib/overview'
import { showToast } from '../lib/toast'

const props = withDefaults(defineProps<{ text: string; copy?: boolean; shown?: string }>(), { copy: true })
const copied = ref(false)

async function doCopy() {
  try {
    await copyText(props.text)
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  } catch (e) {
    showToast(errorMessage(e), 'error')
  }
}
</script>

<template>
  <div class="cmd">
    <code class="pre">{{ shown ?? text }}</code>
    <button v-if="copy" type="button" class="btn copy" @click="doCopy">{{ copied ? 'Copied' : 'Copy' }}</button>
  </div>
</template>

<style scoped>
.cmd { display: flex; align-items: center; gap: 8px; min-height: 34px; box-sizing: border-box; padding: 5px 5px 5px 14px; border-radius: 8px; background: var(--log-bg); color: var(--log-ink); }
code { flex: 1; min-width: 0; font-family: var(--mono); font-size: 13px; overflow-wrap: anywhere; white-space: pre-wrap; user-select: text; }
.btn.copy { flex: none; border-color: transparent; background: color-mix(in srgb, var(--log-ink) 12%, transparent); color: var(--log-ink); }
.btn.copy:hover:not(:disabled) { background: color-mix(in srgb, var(--log-ink) 20%, transparent); }
</style>
