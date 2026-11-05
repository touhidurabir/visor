<script setup lang="ts">
// The sticky action bar at the bottom of the window, shared by Add new
// (Write it and the form) and a config's editor: where saving stands (with
// an icon and a word, never colour alone), what saving does, then the
// buttons (the slot). It stays in view however long the page is.
defineProps<{ tone: 'ok' | 'bad' | 'warn' | 'idle'; status: string; note?: string }>()

const ICONS = {
  ok: 'm5 12 5 5 9-10',
  bad: 'M18 6 6 18M6 6l12 12',
  warn: 'M12 3 2 20h20L12 3zM12 10v4M12 17v.01',
  idle: '',
}
</script>

<template>
  <footer class="savebar" data-testid="save-bar">
    <span :class="['status', `t-${tone}`]" role="status">
      <svg v-if="ICONS[tone]" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="ICONS[tone]" /></svg>
      <strong>{{ status }}</strong>
    </span>
    <span v-if="note" class="note">{{ note }}</span>
    <span class="acts"><slot /></span>
  </footer>
</template>

<style scoped>
/* Full width of the scrolling content area: the negative margins undo
   AppShell's .content padding (28 px at the sides, 40 px at the bottom),
   so the bar sits on the window's bottom edge. WebKit keeps a sticky box
   inside the scroller's padding, so bottom undoes the 40 px there too. */
.savebar {
  position: sticky;
  bottom: -40px;
  z-index: 5;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px 16px;
  /* auto on top: in a flex column page, a short page still has it at the bottom. */
  margin: auto -28px -40px;
  padding: 12px 28px;
  border-top: 1px solid var(--line);
  background: var(--raised);
}
.status { display: inline-flex; align-items: center; gap: 8px; }
.status.t-idle { color: var(--ink2); }
.note { margin-right: auto; color: var(--ink3); font-size: 12.5px; }
.acts { display: inline-flex; flex-wrap: wrap; gap: 8px; margin-left: auto; }
</style>
