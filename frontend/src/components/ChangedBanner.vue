<script setup lang="ts">
// Shown when any config differs from what supervisord runs (or Visor cannot
// tell). "Apply changes" asks the dashboard to run the apply flow.
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import type { ConfigView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { configRoute } from '../lib/links'

const props = defineProps<{ configs: ConfigView[]; busy?: boolean }>()
const emit = defineEmits<{ apply: [] }>()

const changed = computed(() => props.configs.filter((c) => c.Changed))
const uncertain = computed(() => props.configs.filter((c) => c.Uncertain && !c.Changed))
const first = computed(() => changed.value[0] ?? uncertain.value[0])
const diffTo = computed(() => (first.value ? configRoute(first.value.Path, { tab: 'config' }) : '/'))
const headline = computed(() => {
  if (changed.value.length === 1) return `${changed.value[0].FileName} changed on disk`
  if (changed.value.length > 1) return `${changed.value.length} config files changed on disk`
  return uncertain.value.length === 1
    ? `Visor can’t tell if ${uncertain.value[0].FileName} changed`
    : `Visor can’t tell if ${uncertain.value.length} config files changed`
})
const detail = computed(() =>
  changed.value.length
    ? 'since supervisord loaded it. Its processes still run the old settings.'
    : 'Check for changes to find out.',
)
</script>

<template>
  <div class="banner" role="status">
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true">
      <path d="M12 9v4M12 17h.01" /><path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" />
    </svg>
    <span class="msg"><strong>{{ headline }}</strong> {{ detail }}</span>
    <RouterLink v-if="first && first.Path" :to="diffTo">Show diff</RouterLink>
    <button type="button" class="btn" :disabled="busy" @click="emit('apply')">{{ busy ? 'Applying…' : 'Apply changes' }}</button>
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
</style>
