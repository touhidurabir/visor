<script setup lang="ts">
// "What this config does": the plain-English summary of a config and how
// many errors, suggestions, settings and paths the line-by-line view has.
import type { ExplainView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

defineProps<{ view: ExplainView | null; failed?: string | null }>()
const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`
</script>

<template>
  <section class="summary-card" aria-labelledby="summary-title" data-testid="explain-summary">
    <div class="head">
      <h2 id="summary-title">What this config does</h2>
      <span class="muted small">Generated from the file, no AI</span>
    </div>
    <p v-if="failed" class="muted">Visor can’t explain this file. {{ failed }}</p>
    <p v-else-if="!view" class="muted">Loading…</p>
    <p v-else-if="view.ParseError" class="bad">Visor can’t read this file as a config yet: {{ view.ParseError.Message }}</p>
    <template v-else>
      <p class="text">{{ view.Summary }}</p>
      <div class="chips">
        <span v-if="view.Errors" class="chip bad">{{ plural(view.Errors, 'error', 'errors') }}</span>
        <span v-if="view.Suggestions" class="chip warn">{{ plural(view.Suggestions, 'suggestion', 'suggestions') }}</span>
        <span class="chip">{{ plural(view.Settings, 'setting', 'settings') }} explained</span>
        <span v-if="view.Paths" class="chip">{{ plural(view.Paths, 'path', 'paths') }} checked</span>
      </div>
    </template>
  </section>
</template>

<style scoped>
.summary-card { border: 1px solid var(--line); border-radius: 10px; padding: 16px 18px; display: flex; flex-direction: column; gap: 10px; min-width: 0; }
.head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: baseline; gap: 8px; }
h2 { margin: 0; font-size: 15px; }
p { margin: 0; }
.muted { color: var(--text-muted); }
.small { font-size: 12px; }
.text { font-size: 14px; line-height: 1.55; max-width: 900px; user-select: text; }
p.bad { color: var(--failing-fg); }
.chips { display: flex; flex-wrap: wrap; gap: 8px; }
.chip { padding: 3px 10px; border-radius: 999px; background: var(--stopped-bg); color: var(--stopped-fg); font-size: 12px; }
.chip.bad { background: var(--failing-bg); color: var(--failing-fg); font-weight: 600; }
.chip.warn { background: var(--warning-bg); color: var(--warning-fg); font-weight: 600; }
</style>
