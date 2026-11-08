<script setup lang="ts">
// A state as a dot plus a sentence-case word ("Running", "Fatal"), for
// tables and lists. The word takes its tone's colour (green Running, grey
// Stopped); a failing state is semibold too, so the bad rows stand out and
// the healthy ones stay quiet. Use StateBadge where one state has to stand
// out on its own. label replaces the word, keeping the state's colour
// ("2 of 4 running", "Not loaded").
import { computed } from 'vue'
import { stateLook, stateTextClass } from '../lib/state'

const props = defineProps<{ state: string; status?: string; label?: string }>()
const look = computed(() => stateLook(props.state, props.status))
</script>

<template>
  <span class="state">
    <span :class="['dot', `d-${look.tone}`]" aria-hidden="true" />
    <span :class="stateTextClass(look)">{{ label || look.label }}</span>
  </span>
</template>

<style scoped>
.state { display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; }
.alert { font-weight: 600; }
</style>
