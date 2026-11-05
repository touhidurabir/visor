<script setup lang="ts">
// "What this config does": the plain-English summary of a config and how
// many errors, suggestions, settings and paths the line-by-line view has.
// The slot adds more under it (the Add new form's "things to check").
import type { ExplainView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

defineProps<{ view: ExplainView | null; failed?: string | null }>()
const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`
</script>

<template>
  <section class="card" aria-labelledby="summary-title" data-testid="explain-summary">
    <div class="head">
      <h2 id="summary-title">What this config does</h2>
      <span class="meta">From the file, no AI</span>
    </div>
    <p v-if="failed" class="muted">Visor can’t explain this file. {{ failed }}</p>
    <p v-else-if="!view" class="muted">Loading…</p>
    <p v-else-if="view.ParseError" class="t-bad">Visor can’t read this file as a config yet: {{ view.ParseError.Message }}</p>
    <template v-else>
      <p class="text">{{ view.Summary }}</p>
      <div class="counts">
        <span v-if="view.Errors" class="badge b-bad">{{ plural(view.Errors, 'error', 'errors') }}</span>
        <span v-if="view.Suggestions" class="badge b-warn">{{ plural(view.Suggestions, 'suggestion', 'suggestions') }}</span>
        <span class="badge b-idle">{{ plural(view.Settings, 'setting', 'settings') }} explained</span>
        <span v-if="view.Paths" class="badge b-idle">{{ plural(view.Paths, 'path', 'paths') }} checked</span>
      </div>
    </template>
    <slot />
  </section>
</template>

<style scoped>
.card { display: flex; flex-direction: column; gap: 10px; min-width: 0; padding: 16px 18px; border: 1px solid var(--line); border-radius: 10px; background: var(--raised); }
.head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: baseline; gap: 8px 10px; }
h2 { margin: 0; font-size: 15px; font-weight: 600; }
p { margin: 0; }
.meta { color: var(--ink3); font-size: 12.5px; }
.muted { color: var(--ink2); }
.text { max-width: 90ch; color: var(--ink2); line-height: 1.55; user-select: text; }
.counts { display: flex; flex-wrap: wrap; gap: 6px; }
</style>
