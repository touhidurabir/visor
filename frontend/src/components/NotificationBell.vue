<script setup lang="ts">
// The bell at the top of the sidebar: how many notifications are unread,
// and the list itself (NotificationPanel) when clicked.
import { ref } from 'vue'
import { useNotifications } from '../lib/notifications'
import NotificationPanel from './NotificationPanel.vue'

const { unread } = useNotifications()
const open = ref(false)
const bell = ref<HTMLButtonElement>()

function close(refocus: boolean) {
  open.value = false
  if (refocus) bell.value?.focus()
}
</script>

<template>
  <button
    ref="bell"
    type="button"
    class="bell"
    data-testid="notification-bell"
    :aria-label="unread ? `Notifications, ${unread} unread` : 'Notifications'"
    aria-haspopup="dialog"
    :aria-expanded="open"
    @click="open = !open"
  >
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" /><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
    </svg>
    <span v-if="unread" class="badge" aria-hidden="true">{{ unread > 99 ? '99+' : unread }}</span>
  </button>
  <NotificationPanel v-if="open" :anchor="bell" @close="close" />
</template>

<style scoped>
.bell {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  margin-left: auto;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}
.bell:hover, .bell[aria-expanded='true'] { background: var(--bg-hover); color: var(--text); }
.bell:focus-visible { outline: 2px solid var(--focus); outline-offset: 1px; }
.badge {
  position: absolute;
  top: -3px;
  right: -5px;
  min-width: 17px;
  height: 17px;
  padding: 0 4px;
  box-sizing: border-box;
  border-radius: 9px;
  border: 2px solid var(--bg-sidebar);
  background: var(--failing-dot);
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  line-height: 13px;
  text-align: center;
}
</style>
