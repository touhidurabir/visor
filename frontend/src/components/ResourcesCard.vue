<script setup lang="ts">
// CPU and memory of a config file's processes over the last hour (M10):
// now, peak and average per process, and a chart of one of them. Figures
// count each process with its children; Go samples every 10 s.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { UsageView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { ServerService } from '../lib/api'
import { chartPaths, fmtBytes, fmtCpu } from '../lib/usage'

const props = defineProps<{ path: string }>()
const views = ref<UsageView[]>([])
const loaded = ref(false)
const pick = ref('')
const HOUR = 3600_000
const W = 600
const H = 90

async function load() {
  const want = props.path
  let got: UsageView[] = []
  try {
    got = (await ServerService.Usage(want)) ?? []
  } catch {
    // The tab dropped: the card says it is measuring.
  }
  if (want !== props.path) return // a newer page's answer wins
  views.value = got
  loaded.value = true
  if (!views.value.some((v) => v.Process === pick.value)) pick.value = views.value[0]?.Process ?? ''
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  timer = setInterval(() => void load(), 10_000)
})
onBeforeUnmount(() => clearInterval(timer))
watch(() => props.path, () => void load())

const current = (v: UsageView) => v.Points?.[v.Points.length - 1] ?? { CPU: -1, RSS: -1, AtMs: 0 }
const sampled = computed(() => views.value.some((v) => (v.Points ?? []).length > 0))
const kids = (n: number) => (n > 1 ? `with ${n - 1} ${n === 2 ? 'child' : 'children'}` : '')
const peakAvg = (v: UsageView, cpu: boolean) => {
  const s = v.Summary
  if (!(v.Points ?? []).some((p) => (cpu ? p.CPU : p.RSS) >= 0)) return '–'
  return cpu ? `${fmtCpu(s.PeakCPU)} · ${fmtCpu(s.AvgCPU)}` : `${fmtBytes(s.PeakRSS)} · ${fmtBytes(s.AvgRSS)}`
}

const chosen = computed(() => views.value.find((v) => v.Process === pick.value))
const toMs = computed(() => current(chosen.value ?? ({ Points: [] } as unknown as UsageView)).AtMs || Date.now())

// The top of a chart: the next round step above the peak.
function ceilTo(v: number, steps: number[]): number {
  for (const s of steps) if (v <= s) return s
  return Math.ceil(v / steps[steps.length - 1]) * steps[steps.length - 1]
}
const MB = 1024 * 1024
const cpuTop = computed(() => ceilTo(chosen.value?.Summary.PeakCPU ?? 0, [10, 25, 50, 100, 200, 400, 800]))
const memTop = computed(() => ceilTo(chosen.value?.Summary.PeakRSS ?? 0, [16 * MB, 64 * MB, 128 * MB, 256 * MB, 512 * MB, 1024 * MB, 2048 * MB, 4096 * MB]))
const cpuLines = computed(() => chartPaths((chosen.value?.Points ?? []).map((p) => ({ AtMs: p.AtMs, v: p.CPU })), toMs.value - HOUR, toMs.value, cpuTop.value, W, H))
const memLines = computed(() => chartPaths((chosen.value?.Points ?? []).map((p) => ({ AtMs: p.AtMs, v: p.RSS })), toMs.value - HOUR, toMs.value, memTop.value, W, H))
const charts = computed(() => [
  { id: 'cpu', label: 'CPU %', lines: cpuLines.value, top: `${cpuTop.value} %`, mid: `${cpuTop.value / 2} %` },
  { id: 'mem', label: 'Memory', lines: memLines.value, top: fmtBytes(memTop.value), mid: fmtBytes(memTop.value / 2) },
])
</script>

<template>
  <section class="card" aria-labelledby="resources-title" data-testid="resources">
    <div class="head">
      <h2 id="resources-title">Resources</h2>
      <span class="meta">Last hour, every 10 seconds, with the children each process starts</span>
    </div>
    <p v-if="!loaded || !sampled" class="meta none">Measuring… the first figures appear within 10 seconds.</p>
    <template v-else>
      <div class="scroll">
        <table class="tbl">
          <thead>
            <tr>
              <th>Process</th>
              <th class="r">CPU now</th>
              <th class="r">peak · avg</th>
              <th class="r">Memory now</th>
              <th class="r">peak · avg</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="v in views" :key="v.Process" :data-usage-row="v.Process">
              <td>
                <span class="mono">{{ v.Process }}</span> <span class="meta">{{ current(v).RSS >= 0 ? kids(v.Summary.Procs) : '' }}</span>
              </td>
              <td class="r mono">{{ current(v).RSS >= 0 && current(v).CPU < 0 ? '…' : fmtCpu(current(v).CPU) }}</td>
              <td class="r mono meta">{{ peakAvg(v, true) }}</td>
              <td class="r mono">{{ fmtBytes(current(v).RSS) }}</td>
              <td class="r mono meta">{{ peakAvg(v, false) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="charts">
        <label v-if="views.length > 1" class="pick">Chart <select v-model="pick" aria-label="Process to chart">
          <option v-for="v in views" :key="v.Process" :value="v.Process">{{ v.Process }}</option>
        </select></label>
        <div v-for="c in charts" :key="c.id" class="chart">
          <div class="meta">{{ c.label }}</div>
          <svg :viewBox="`0 0 ${W + 56} ${H + 4}`" role="img" :aria-label="`${c.label} of ${pick} over the last hour`" :data-chart="c.id">
            <line x1="0" :x2="W" y1="2" y2="2" class="grid" /><text :x="W + 6" y="10">{{ c.top }}</text>
            <line x1="0" :x2="W" :y1="2 + H / 2" :y2="2 + H / 2" class="grid" /><text :x="W + 6" :y="6 + H / 2">{{ c.mid }}</text>
            <line x1="0" :x2="W" :y1="2 + H" :y2="2 + H" class="grid" />
            <g transform="translate(0 2)"><polyline v-for="(l, i) in c.lines" :key="i" :points="l" class="line" /></g>
          </svg>
        </div>
        <div class="axis" aria-hidden="true"><span>60 min ago</span><span>30 min</span><span>now</span></div>
        <p class="meta foot">A gap is time the process wasn’t running. Memory is resident memory; pages shared between workers count in each.</p>
      </div>
    </template>
  </section>
</template>

<style scoped>
.card { display: flex; flex-direction: column; gap: 10px; padding: 16px 18px; border: 1px solid var(--line); border-radius: 10px; background: var(--raised); }
.head { display: flex; flex-wrap: wrap; align-items: baseline; gap: 4px 10px; }
h2 { margin: 0; font-size: 15px; font-weight: 600; }
.meta { color: var(--ink3); font-size: 12.5px; }
.mono { font-family: var(--mono); font-size: 12.5px; }
.none { margin: 0; }
.scroll { overflow-x: auto; margin: 0 -18px; }
.tbl { width: 100%; border-collapse: collapse; font-size: 13px; }
.tbl th { text-align: left; font-size: 12px; font-weight: 600; color: var(--ink3); padding: 6px 18px; border-bottom: 1px solid var(--line); white-space: nowrap; }
.tbl td { padding: 8px 18px; border-top: 1px solid var(--line); white-space: nowrap; font-variant-numeric: tabular-nums; }
.tbl tr:first-child td { border-top: 0; }
.tbl .r { text-align: right; }
.charts { display: flex; flex-direction: column; gap: 8px; padding-top: 6px; border-top: 1px solid var(--line); }
.pick { display: flex; align-items: center; gap: 8px; font-size: 13px; }
.chart svg { display: block; width: 100%; height: auto; overflow: visible; }
.grid { stroke: var(--line); stroke-width: 1; vector-effect: non-scaling-stroke; }
.line { fill: none; stroke: var(--accent); stroke-width: 1.5; stroke-linejoin: round; vector-effect: non-scaling-stroke; }
text { font-size: 10.5px; fill: var(--ink3); }
.axis { display: flex; justify-content: space-between; padding-right: 8.5%; font-size: 11px; color: var(--ink3); }
.foot { margin: 0; }
</style>
