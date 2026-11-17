<script setup lang="ts">
// A server report's checks, as Doctor shows its own: a mark, the title and
// detail, the status in words, and under a failing row the steps that fix
// it. Used by the server form (Test, a failed Connect) and Server report.
import type { ReportRow } from '../../bindings/github.com/touhidurabir/visor/internal/doctor/models'
import { reportLook } from '../lib/doctor'
import CommandBlock from './CommandBlock.vue'

defineProps<{ rows: ReportRow[] }>()
</script>

<template>
  <ol class="checks" aria-label="What Visor checked">
    <li v-for="r in rows" :key="r.ID" class="step" :data-row="r.ID" :data-status="r.Status">
      <span :class="['mark', `m-${reportLook(r.Status).tone}`]" aria-hidden="true">{{ reportLook(r.Status).glyph }}</span>
      <span class="step-text">
        <span :class="['step-title', { 't-wait': r.Status === 'skipped' }]">{{ r.Title }}</span>
        <span v-if="r.Detail" class="detail">{{ r.Detail }}</span>
        <ol v-if="r.Fix?.length" class="fix">
          <li v-for="(f, i) in r.Fix" :key="i">
            <p>{{ f.Text }}</p>
            <CommandBlock v-if="f.Command" :text="f.Command" />
          </li>
        </ol>
      </span>
      <span :class="['badge', 'word', `b-${reportLook(r.Status).tone}`]">{{ reportLook(r.Status).word }}</span>
    </li>
  </ol>
</template>

<style scoped>
.checks { list-style: none; margin: 0; padding: 4px 0; }
.step { display: grid; grid-template-columns: 26px minmax(0, 1fr) auto; column-gap: 12px; align-items: start; padding: 10px 16px; border-top: 1px solid var(--line); }
.step:first-child { border-top: 0; }
.step-text { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.step-title { font-weight: 600; }
.t-wait { color: var(--ink3); }
.detail { color: var(--ink2); font-size: 12.5px; overflow-wrap: anywhere; user-select: text; }
.word { height: 20px; font-size: 11.5px; white-space: nowrap; }
.mark { width: 24px; height: 24px; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 13px; font-weight: 700; }
.m-ok { background: var(--ok-soft); color: var(--ok); }
.m-bad { background: var(--bad-soft); color: var(--bad); }
.m-warn { background: var(--warn-soft); color: var(--warn); }
.m-idle { background: var(--idle-soft); color: var(--idle); }
.fix { margin: 8px 0 2px; padding-left: 18px; display: flex; flex-direction: column; gap: 8px; }
.fix p { margin: 0 0 6px; color: var(--ink2); }
</style>
