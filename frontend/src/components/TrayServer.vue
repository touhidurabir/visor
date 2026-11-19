<script setup lang="ts">
// One open server in the menu-bar popover (board "Popover with servers"):
// a header that folds (colour, name, tag, how it is) and, unfolded, its
// processes with the buttons that apply, Restart all on it and Open tab. A
// production server asks first (lib/production.ts); a Read only one shows
// no buttons.
import { computed, ref } from 'vue'
import { ShellService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { forTab } from '../lib/api'
import { connectionProblem } from '../lib/connection'
import { errorMessage, useOverview } from '../lib/overview'
import { tagOf } from '../lib/servers'
import { guard, withConsent } from '../lib/production'
import { VERB_LABEL, actionTargets, itemActions, itemLook, trayGroups, trayHeader, type TrayItem, type TrayVerb } from '../lib/tray'
import type { TabInfo } from '../lib/tabs'

const props = defineProps<{ tab: TabInfo }>()
const { overview } = useOverview(() => props.tab.id)
const { ServerService } = forTab(props.tab.id)

const open = ref(false)
const busy = ref(false)
const error = ref('')
const problem = computed(() => connectionProblem(overview.value?.Server))
const head = computed(() => trayHeader(overview.value?.Server, overview.value?.Stats, problem.value))
const readOnly = computed(() => props.tab.readOnly || !!overview.value?.Host?.ReadOnly)
const items = computed(() => trayGroups(overview.value?.Configs ?? []).flatMap((g) => g.items))
const chip = computed(() => tagOf(props.tab.tag))

const calls: Record<TrayVerb, (name: string) => Promise<void>> = {
  start: (n) => ServerService.Start(n),
  stop: (n) => ServerService.Stop(n),
  restart: (n) => ServerService.Restart(n),
}

async function act(fn: () => Promise<unknown>) {
  busy.value = true
  error.value = ''
  try {
    await fn()
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}

const METHOD: Record<TrayVerb, string> = { start: 'Start', stop: 'Stop', restart: 'Restart' }

/**
 * A row's button: on every copy it applies to, with one production
 * question for the row, not one per copy.
 */
const run = (v: TrayVerb, item: TrayItem) =>
  act(async () => {
    const targets = actionTargets(v, item)
    if (!targets.length) return
    await guard(props.tab.id, METHOD[v], [item.multi ? `every copy of ${item.name}` : item.name])
    await withConsent(() => Promise.all(targets.map((p) => calls[v](p.FullName))), props.tab.id)
  })
</script>

<template>
  <section class="srv" :aria-label="tab.name" :data-server="tab.id">
    <button class="head" type="button" :aria-expanded="open" @click="open = !open">
      <span class="dot" :style="{ background: tab.color }" aria-hidden="true" />
      <span class="who">
        <span class="line"><strong>{{ tab.name }}</strong><span :class="['chip', chip.cls]">{{ chip.chip }}</span></span>
        <span :class="['sub', head.badge ? 't-bad' : problem ? 't-warn' : 'meta']">{{ head.badge ?? head.meta }}</span>
      </span>
      <svg :class="['chev', { on: open }]" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 6l6 6-6 6" /></svg>
    </button>
    <div v-if="open" class="body">
      <p v-if="problem" class="meta pad">{{ problem.title }}</p>
      <div v-for="item in problem ? [] : items" :key="item.key" class="row" data-testid="tray-server-row">
        <span :class="['dot', `d-${itemLook(item).tone}`]" aria-hidden="true" />
        <span class="who">
          <span class="name">{{ item.name }}<template v-if="item.multi"> ×{{ item.procs.length }}</template></span>
          <span :class="['sub', itemLook(item).lineClass]">{{ itemLook(item).line }}</span>
        </span>
        <template v-if="!readOnly">
          <button v-for="v in itemActions(item)" :key="v" class="btn sm" type="button" :disabled="busy" :aria-label="`${VERB_LABEL[v]} ${item.name} on ${tab.name}`" @click="run(v, item)">
            {{ VERB_LABEL[v] }}
          </button>
        </template>
      </div>
      <p v-if="error" class="err" role="alert">{{ error }}</p>
      <div class="foot">
        <button v-if="!readOnly && !problem" class="btn sm" type="button" :disabled="busy" @click="act(() => ServerService.RestartAll())">Restart all on {{ tab.name }}</button>
        <span class="grow" />
        <button class="link" type="button" @click="ShellService.OpenTab(tab.id)">Open tab</button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.srv { border-top: 1px solid var(--line); padding: 4px 6px; }
.head { display: flex; align-items: center; gap: 8px; width: 100%; min-height: 40px; padding: 6px 8px 6px 10px; border: 0; border-radius: 8px; background: transparent; color: var(--ink); font: inherit; text-align: left; cursor: pointer; }
.head:hover { background: var(--hover); }
.who { display: flex; flex-direction: column; min-width: 0; flex: 1; }
.line { display: flex; align-items: center; gap: 6px; }
.chip { display: inline-flex; align-items: center; height: 17px; padding: 0 5px; border-radius: 5px; font-size: 10.5px; font-weight: 700; letter-spacing: .02em; }
.sub { font-size: 12px; line-height: 16px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.chev { transition: transform .15s; color: var(--ink3); flex: none; }
.chev.on { transform: rotate(90deg); }
.body { display: flex; flex-direction: column; gap: 2px; padding: 0 2px 6px 6px; }
.row { display: flex; align-items: center; gap: 10px; padding: 6px 8px 6px 10px; border-radius: 8px; }
.name { font-family: var(--mono); font-size: 12.5px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.foot { display: flex; align-items: center; gap: 8px; padding: 6px 8px 0 10px; }
.grow { flex: 1; }
.link { border: 0; background: none; padding: 0; color: var(--accent-text); font: inherit; font-size: 12.5px; cursor: pointer; }
.err { color: var(--bad); margin: 4px 10px; font-size: 12px; }
.pad { margin: 4px 10px; }
</style>
