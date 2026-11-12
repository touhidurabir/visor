<script setup lang="ts">
// Shown when a config differs from what supervisord runs: changed on disk
// since it was loaded, not loaded at all (added outside Visor, before
// anyone ran update), or Visor can't tell. "Apply changes" asks the
// dashboard to run the apply flow; a single file nobody loaded gets "Load"
// for just that file. Hide (×) puts it away until the set of files it is
// about changes; the rows keep their "changed" badge and Load button.
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import type { ConfigView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { configRoute } from '../lib/links'
import { bannerKey, dismissBanner, isBannerDismissed } from '../lib/changed-banner'

const props = defineProps<{ configs: ConfigView[]; busy?: boolean }>()
const emit = defineEmits<{ apply: []; load: [c: ConfigView] }>()

/** Files supervisord runs nothing from yet. */
const unloaded = computed(() => props.configs.filter((c) => c.Path && (c.NotLoaded ?? []).length > 0 && !(c.Processes ?? []).length))
const isUnloaded = (c: ConfigView) => unloaded.value.includes(c)
const changed = computed(() => props.configs.filter((c) => c.Changed && !isUnloaded(c)))
const uncertain = computed(() => props.configs.filter((c) => c.Uncertain && !c.Changed && !isUnloaded(c)))

const key = computed(() => bannerKey(changed.value, unloaded.value, uncertain.value))
const shown = computed(() => key.value !== '' && !isBannerDismissed(key.value))

const files = (n: number, one: string) => (n === 1 ? one : `${n} config files`)
const headline = computed(() => {
  const parts: string[] = []
  const c = changed.value
  const u = unloaded.value
  if (c.length) parts.push(`${files(c.length, c[0].FileName)} changed on disk`)
  if (u.length) parts.push(`${files(u.length, u[0].FileName)} ${u.length === 1 ? 'isn’t' : 'aren’t'} loaded`)
  // "x changed on disk" runs on into "since supervisord loaded it"; the
  // other headlines end before the detail.
  if (parts.length) return parts.join(', and ') + (unloaded.value.length ? ':' : '')
  const n = uncertain.value.length
  return `Visor can’t tell if ${files(n, uncertain.value[0]?.FileName ?? '')} changed.`
})
const detail = computed(() => {
  const c = changed.value.length
  const u = unloaded.value.length
  if (c && u) return 'supervisord still runs the old settings, and doesn’t know the new programs yet.'
  if (c) return c === 1 ? 'since supervisord loaded it. Its processes still run the old settings.' : 'since supervisord loaded them. Their processes still run the old settings.'
  if (u) return u === 1 ? 'supervisord doesn’t know its programs yet.' : 'supervisord doesn’t know their programs yet.'
  return 'Check for changes to find out.'
})
const diffTo = computed(() => (changed.value[0] ? configRoute(changed.value[0].Path, { tab: 'config' }) : ''))
/** Just one file nobody loaded: Load applies only that file. */
const loadOne = computed(() => (unloaded.value.length === 1 && !changed.value.length && !uncertain.value.length ? unloaded.value[0] : null))
</script>

<template>
  <div v-if="shown" class="banner" role="status" data-testid="changed-banner">
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true">
      <path d="M12 9v4M12 17h.01" /><path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" />
    </svg>
    <span class="msg"><strong>{{ headline }}</strong> {{ detail }}</span>
    <RouterLink v-if="diffTo" :to="diffTo">Show diff</RouterLink>
    <button v-if="loadOne" type="button" class="btn" :disabled="busy" @click="emit('load', loadOne)">{{ busy ? 'Loading…' : 'Load' }}</button>
    <button v-else type="button" class="btn" :disabled="busy" @click="emit('apply')">{{ busy ? 'Applying…' : 'Apply changes' }}</button>
    <button type="button" class="btn quiet icon sm close" aria-label="Hide this until something else changes" title="Hide this until something else changes" @click="dismissBanner(key)">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" /></svg>
    </button>
  </div>
</template>

<style scoped>
.banner {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-radius: var(--radius-md);
  background: var(--warning-bg);
  color: var(--warning-fg);
}
.msg { flex: 1 1 300px; }
a { color: inherit; font-weight: 600; }
.close { color: inherit; }
</style>
