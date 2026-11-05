<script setup lang="ts">
// The notification list: Unread / All, grouped Today / Earlier, mark as
// read, a footer note when macOS is not showing Visor's banners, and each
// notification leading to its config. Each item leads with its kind icon;
// a severity glyph sits before the title (with the word for screen
// readers), and the blue dot marks it unread. Escape or a click outside
// closes it.
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { NotifyService, type NotificationView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { groupByDay, noticeIcon, noticeRoute, noticeTime, severityLevel, severityWord, useNotifications } from '../lib/notifications'
import { describeError } from '../lib/faults'
import { showToast } from '../lib/toast'

const props = defineProps<{ anchor?: HTMLElement }>()
const emit = defineEmits<{ close: [refocus: boolean] }>()

const router = useRouter()
const { items, unread, loaded, failed, markRead, markAllRead, reload } = useNotifications()
const tab = ref<'unread' | 'all'>(unread.value ? 'unread' : 'all')
const shown = computed(() => (tab.value === 'unread' ? items.value.filter((n) => !n.Read) : items.value))
const permission = ref('')
const nowMs = ref(Date.now())
const groups = computed(() => groupByDay(shown.value, nowMs.value))
const panel = ref<HTMLElement>()

async function checkPermission() {
  try {
    permission.value = await NotifyService.NotificationStatus()
  } catch {
    permission.value = 'unavailable'
  }
}

async function allow() {
  try {
    await NotifyService.OpenNotificationSettings()
  } catch (e) {
    showToast(describeError(e), 'error')
  }
}

function go(n: NotificationView) {
  void markRead(n.ID)
  void router.push(noticeRoute(n))
  emit('close', false)
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    emit('close', true)
  }
}
function onPointer(e: PointerEvent) {
  const t = e.target as Node
  if (panel.value?.contains(t) || props.anchor?.contains(t)) return
  emit('close', false)
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  void checkPermission()
  window.addEventListener('focus', checkPermission)
  document.addEventListener('pointerdown', onPointer, true)
  timer = setInterval(() => (nowMs.value = Date.now()), 30_000)
  await nextTick()
  panel.value?.focus()
})
onBeforeUnmount(() => {
  window.removeEventListener('focus', checkPermission)
  document.removeEventListener('pointerdown', onPointer, true)
  clearInterval(timer)
})
</script>

<template>
  <Teleport to="body">
    <section
      ref="panel"
      class="panel"
      role="dialog"
      aria-labelledby="notif-title"
      tabindex="-1"
      data-testid="notification-panel"
      @keydown="onKey"
    >
      <header class="head">
        <h2 id="notif-title">Notifications</h2>
        <button type="button" class="btn quiet sm mark-all" :disabled="!unread" @click="markAllRead">Mark all as read</button>
        <RouterLink to="/settings" class="btn quiet icon sm" aria-label="Notification settings" title="Notification settings" @click="emit('close', false)">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
          </svg>
        </RouterLink>
      </header>

      <div class="seg kinds">
        <button type="button" :aria-pressed="tab === 'unread'" @click="tab = 'unread'">Unread · {{ unread }}</button>
        <button type="button" :aria-pressed="tab === 'all'" @click="tab = 'all'">All</button>
      </div>

      <div class="scroller">
        <p v-if="failed" class="empty">
          Couldn’t load notifications. {{ failed }}
          <button type="button" class="link" @click="reload">Try again</button>
        </p>
        <p v-else-if="!loaded" class="empty">Loading…</p>
        <p v-else-if="!shown.length" class="empty">
          {{ tab === 'unread' ? 'You’re all caught up.' : 'No notifications yet. Visor adds one when a process in a watched config fails.' }}
        </p>
        <template v-else>
          <section v-for="g in groups" :key="g.name" class="group" :aria-labelledby="`notif-${g.name}`">
            <h3 :id="`notif-${g.name}`" class="group-name">{{ g.name }}</h3>
            <ul class="list">
              <li v-for="n in g.items" :key="n.ID">
                <button type="button" :class="['item', `sev-${severityLevel(n.Severity)}`, { unread: !n.Read }]" @click="go(n)">
                  <span class="kind" aria-hidden="true">
                    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path :d="noticeIcon(n)" /></svg>
                  </span>
                  <span class="body">
                    <span class="title">
                      <svg class="sev" :data-severity="severityLevel(n.Severity)" width="13" height="13" viewBox="0 0 16 16" aria-hidden="true">
                        <path v-if="severityLevel(n.Severity) === 'warning'" class="shape" d="M8 1.2 15.2 14H.8Z" />
                        <circle v-else class="shape" cx="8" cy="8" r="7" />
                        <path v-if="severityLevel(n.Severity) === 'info'" class="mark" d="M8 7.2v4.3M8 4.6v.01" />
                        <path v-else-if="severityLevel(n.Severity) === 'warning'" class="mark" d="M8 6v3.6M8 11.8v.01" />
                        <path v-else class="mark" d="M8 4.4v4.4M8 11.4v.01" />
                      </svg><span class="sr">{{ severityWord(n.Severity) }}: </span>{{ n.Title }}<span v-if="!n.Read" class="sr">, unread</span>
                    </span>
                    <span v-if="n.Detail" class="detail">{{ n.Detail }}</span>
                    <span class="meta">{{ [n.ConfigName, noticeTime(n.AtMs, nowMs)].filter(Boolean).join(' · ') }}</span>
                  </span>
                  <span class="unread-mark" aria-hidden="true"><span v-if="!n.Read" class="dot" /></span>
                </button>
              </li>
            </ul>
          </section>
        </template>
      </div>

      <footer class="foot">
        <RouterLink to="/issues" class="all" @click="emit('close', false)">Open all issues</RouterLink>
        <p v-if="permission === 'denied'" class="note warn" data-testid="notify-blocked">
          macOS is blocking Visor’s banners, so these only show here.
          <button type="button" class="link" @click="allow">Allow in System Settings</button>
        </p>
        <p v-else-if="permission === 'unavailable'" class="note">This build can’t show macOS banners, so notifications show only here.</p>
      </footer>
    </section>
  </Teleport>
</template>

<style scoped>
.panel {
  position: fixed;
  z-index: 30;
  top: 12px;
  left: 228px;
  width: 400px;
  max-height: calc(100vh - 24px);
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--raised);
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  box-shadow: var(--menu-shadow);
  color: var(--ink);
  outline: none;
}
.head { display: flex; align-items: center; gap: 8px; padding: 14px 14px 10px 16px; }
h2 { margin: 0 auto 0 0; font-size: 15px; font-weight: 650; }
.mark-all:not(:disabled) { color: var(--accent-text); }
.kinds { display: flex; margin: 0 16px 10px; }
.kinds > button { flex: 1; justify-content: center; }
.scroller { flex: 1 1 auto; min-height: 0; overflow-y: auto; border-top: 1px solid var(--line); }
.empty { margin: 0; padding: 18px 16px; color: var(--ink2); text-align: center; line-height: 1.45; }
.group-name { margin: 0; padding: 10px 16px 4px; font-size: 12px; font-weight: 600; color: var(--ink3); }
.list { list-style: none; margin: 0; padding: 0 8px; }
.group:last-child .list { padding-bottom: 6px; }
.item { display: grid; grid-template-columns: 30px minmax(0, 1fr) 10px; gap: 10px; width: 100%; padding: 10px 8px; border: 0; border-radius: 8px; background: transparent; color: var(--ink); font: inherit; text-align: left; cursor: pointer; }
.item:hover { background: var(--hover); }
.item:focus-visible { outline: 2px solid var(--focus); outline-offset: -2px; }
/* The kind icon on a tint of the severity's colour. */
.kind { display: flex; align-items: center; justify-content: center; width: 30px; height: 30px; border-radius: 50%; }
.sev-error .kind { background: var(--bad-soft); color: var(--bad); }
.sev-warning .kind { background: var(--warn-soft); color: var(--warn); }
.sev-info .kind { background: var(--accent-soft); color: var(--accent-text); }
.body { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
/* Unread reads bold, read regular, as in macOS Notification Center. */
.title { font-weight: 400; overflow-wrap: anywhere; }
.item.unread .title { font-weight: 600; }
/* Severity before the title: a red circle, an amber triangle or a blue
   circle with its mark knocked out, plus the word for screen readers. */
.sev { display: inline-block; margin: 0 5px 0 0; vertical-align: -1px; }
.sev .shape { fill: currentColor; }
.sev .mark { fill: none; stroke: var(--raised); stroke-width: 2; stroke-linecap: round; }
.sev[data-severity='error'] { color: var(--bad); }
.sev[data-severity='warning'] { color: var(--warn); }
.sev[data-severity='info'] { color: var(--accent-text); }
.detail { color: var(--ink2); font-size: 13px; overflow-wrap: anywhere; }
.meta { color: var(--ink3); font-size: 12.5px; }
.unread-mark { padding-top: 6px; }
.unread-mark .dot { background: var(--accent); }
.foot { display: flex; flex-direction: column; gap: 4px; padding: 10px 16px; border-top: 1px solid var(--line); }
.foot .all { align-self: flex-start; color: var(--accent-text); font-weight: 600; text-decoration: none; }
.foot .all:hover { text-decoration: underline; }
.note { margin: 0; color: var(--ink3); font-size: 12.5px; line-height: 1.4; }
.note.warn { color: var(--warn); }
.link { padding: 0; border: 0; background: none; color: var(--accent-text); font: inherit; cursor: pointer; text-decoration: none; }
.note.warn .link { color: inherit; font-weight: 600; text-decoration: underline; }
.link:hover { text-decoration: underline; }
.sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
</style>
