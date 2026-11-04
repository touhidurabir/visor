<script setup lang="ts">
// A state as a dot plus a sentence-case word ("Running", "Fatal"), for
// tables and lists. Only a failing state colours its word, in semibold, so
// the bad rows stand out and the healthy ones stay quiet. Use StateBadge
// where one state has to stand out on its own.
import { computed } from 'vue'
import { stateLook } from '../lib/state'

const props = defineProps<{ state: string; status?: string }>()
const look = computed(() => stateLook(props.state, props.status))
</script>

<template>
  <span class="state">
    <span :class="['dot', `d-${look.tone}`]" aria-hidden="true" />
    <span :class="look.alert ? ['alert', `t-${look.tone}`] : undefined">{{ look.label }}</span>
  </span>
</template>

<style scoped>
.state { display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; }
.alert { font-weight: 600; }
</style>
