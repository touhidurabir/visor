<script setup lang="ts">
// One state as a tinted badge: dot plus word, never colour alone (spec §9).
// For the places where a single state has to stand out; tables and lists
// use StateText. `status` is the Go side's running/failing/stopped
// classification; without it the state name decides (lib/state.ts).
import { computed } from 'vue'
import { stateLook } from '../lib/state'

const props = defineProps<{ state: string; status?: string }>()
const look = computed(() => stateLook(props.state, props.status))
</script>

<template>
  <span :class="['badge', `b-${look.tone}`]"><span :class="['dot', `d-${look.tone}`]" aria-hidden="true" />{{ look.label }}</span>
</template>
