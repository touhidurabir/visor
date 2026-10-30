<script setup lang="ts">
// Temporary dashboard: configs with their processes, kept live by the
// visor:overview event. Task 15 replaces it with the designed dashboard.
import { onMounted, onUnmounted, ref } from 'vue'
import { Events } from '@wailsio/runtime'
import { ServerService, type OverviewView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { VisorOverview } from '../../bindings/events'

const overview = ref<OverviewView | null>(null)
const loadError = ref('')

async function load() {
  try {
    overview.value = await ServerService.Overview()
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

let stop: (() => void) | undefined
onMounted(() => {
  stop = Events.On(VisorOverview, (ev) => {
    overview.value = ev.data
    loadError.value = ''
  })
  void load()
})
onUnmounted(() => stop?.())
</script>

<template>
  <section>
    <div v-if="loadError" class="error" role="alert">
      <strong>Cannot load the dashboard</strong>
      <p>{{ loadError }}</p>
      <button type="button" @click="load">Try again</button>
    </div>
    <p v-else-if="!overview" class="muted">Loading…</p>
    <template v-else>
      <p class="server">
        <span :class="['dot', 'health-' + overview.Server.Health]" :title="'Health: ' + overview.Server.Health" />
        {{ overview.Server.Name }}
        <span v-if="overview.Server.Version" class="muted">supervisord {{ overview.Server.Version }}</span>
        <span class="muted">{{ overview.Server.ConfigPath }}</span>
      </p>
      <div v-if="overview.Server.Error" class="error" role="alert">
        <strong>Cannot reach supervisord</strong>
        <p>{{ overview.Server.Error }}</p>
      </div>
      <p class="muted stats">
        {{ overview.Stats.Configs }} configs · {{ overview.Stats.Processes }} processes ·
        {{ overview.Stats.Running }} running · {{ overview.Stats.Failing }} failing ·
        {{ overview.Stats.Stopped }} stopped · {{ overview.Stats.OpenIssues }} open issues
      </p>
      <p v-if="!overview.Configs?.length" class="muted">No configs found.</p>
      <table v-else>
        <thead>
          <tr><th>Name</th><th>State</th><th>For</th><th>PID</th><th>Details</th></tr>
        </thead>
        <tbody v-for="c in overview.Configs ?? []" :key="c.Path || c.FileName">
          <tr class="config">
            <td colspan="5">
              <strong>{{ c.FileName }}</strong>
              <span v-if="c.Changed" class="badge">changed</span>
              <span class="muted">{{ c.Summary }}</span>
            </td>
          </tr>
          <tr v-for="p in c.Processes ?? []" :key="p.FullName">
            <td class="name">{{ p.Group === p.Name ? p.Name : p.FullName }}</td>
            <td :class="['state', p.State.toLowerCase()]">{{ p.State }}</td>
            <td class="muted">{{ p.Since }}</td>
            <td>{{ p.PID || '—' }}</td>
            <td class="muted">{{ p.Detail }}</td>
          </tr>
        </tbody>
      </table>
    </template>
  </section>
</template>

<style scoped>
.muted { color: var(--text-muted); }
.server { display: flex; gap: 8px; align-items: center; margin: 0 0 4px; }
.stats { margin: 0 0 12px; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-muted); }
.dot.health-ok { background: #1a7f37; }
.dot.health-warning { background: #bf8700; }
.dot.health-error { background: #cf222e; }
table { width: 100%; border-collapse: collapse; }
th, td { text-align: left; padding: 4px 8px; }
th { color: var(--text-muted); font-weight: 500; }
tr.config td { padding-top: 12px; }
tr.config .muted { margin-left: 8px; }
td.name { padding-left: 20px; }
.badge { margin-left: 6px; padding: 0 6px; border-radius: 8px; background: #bf870033; font-size: 11px; }
.state.running { color: #1a7f37; }
.state.fatal, .state.backoff { color: #cf222e; }
.error { padding: 12px; margin-bottom: 12px; border: 1px solid #cf222e; border-radius: 6px; }
.error p { margin: 4px 0 8px; user-select: text; }
</style>
