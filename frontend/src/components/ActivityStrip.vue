<script setup lang="ts">
// The dashboard's strip for the latest change made outside Visor in the last
// day (not a file change: the changed-on-disk banner says those) ("web:web restarted outside Visor · 2 min ago · Activity"). It can be
// hidden until something else changes.
import { computed, ref } from 'vue'
import { useActivity } from '../lib/activity'
import { noticeTime } from '../lib/notifications'
import { tabHref } from '../lib/tabs'

const { events } = useActivity(undefined, 50)
const hidden = ref('')
const latest = computed(() => {
  // Files changed on disk have their own banner (ChangedBanner) with Apply.
  const e = events.value.find((x) => !x.ByVisor && x.Group !== 'files')
  if (!e || Date.now() - e.AtMs > 24 * 3600_000 || e.ID === hidden.value) return null
  return e
})
</script>

<template>
  <div v-if="latest" class="strip" role="status" data-testid="activity-strip">
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M12 8v4l3 2" /><circle cx="12" cy="12" r="9" /></svg>
    <span class="txt"><strong>{{ latest.Text }}</strong> outside Visor · {{ noticeTime(latest.AtMs, Date.now()) }}</span>
    <RouterLink :to="tabHref('/activity')" class="go">Activity</RouterLink>
    <button type="button" class="btn quiet icon sm" aria-label="Hide this until something else changes" @click="hidden = latest.ID">
      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" aria-hidden="true"><path d="M5 5l14 14M19 5L5 19" /></svg>
    </button>
  </div>
</template>

<style scoped>
.strip { display: flex; align-items: center; gap: 10px; padding: 9px 12px 9px 14px; border: 1px solid var(--accent-soft); border-radius: 10px; background: var(--accent-soft); color: var(--accent-text); }
.txt { flex: 1; min-width: 0; }
.txt strong { font-family: var(--mono); font-weight: 600; }
.go { font-weight: 600; color: inherit; }
</style>
