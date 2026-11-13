<script setup lang="ts">
// The four kinds of change made outside Visor, each a checkbox: checked
// notifies (the default), unchecked mutes it.
import type { ActivityMutes } from '../../bindings/github.com/touhidurabir/visor/internal/settings/models'

const props = defineProps<{ mutes: ActivityMutes }>()
const emit = defineEmits<{ change: [key: keyof ActivityMutes, muted: boolean] }>()

const KINDS: { key: keyof ActivityMutes; label: string }[] = [
  { key: 'processes', label: 'A process is started, stopped or restarted' },
  { key: 'programs', label: 'Programs are added or removed (update)' },
  { key: 'files', label: 'A config file is added, changed or removed on disk' },
  { key: 'daemon', label: 'supervisord restarts or shuts down' },
]
</script>

<template>
  <fieldset class="mutes">
    <legend class="meta">Notify me when, outside Visor</legend>
    <label v-for="k in KINDS" :key="k.key">
      <input type="checkbox" :checked="!props.mutes[k.key]" @change="emit('change', k.key, !($event.target as HTMLInputElement).checked)" />
      {{ k.label }}
    </label>
  </fieldset>
</template>

<style scoped>
.mutes { display: flex; flex-direction: column; gap: 6px; margin: 0; padding: 0; border: 0; }
legend { margin-bottom: 4px; }
label { display: flex; align-items: center; gap: 8px; }
</style>
