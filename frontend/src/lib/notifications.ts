// Visor's own notification list (the bell): one shared copy, loaded once
// and kept current by visor:notifications events. Banners from macOS are
// extra; everything Visor notifies about lands here with read/unread state.
import { computed, ref } from 'vue'
import { Events } from '@wailsio/runtime'
import type { RouteLocationRaw } from 'vue-router'
import { InboxService, type NotificationView, type NotificationsView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { VisorNotifications } from '../../bindings/events'
import { historyTime } from './format'
import { configRoute } from './links'

const items = ref<NotificationView[]>([])
const loaded = ref(false)
const failed = ref<string | null>(null)
let started = false
let fromEvent = false

function apply(v: NotificationsView) {
  items.value = v.Items ?? []
  loaded.value = true
  failed.value = null
}

/** Starts following the list (once per window); later calls share it. */
export function useNotifications() {
  if (!started) {
    started = true
    Events.On(VisorNotifications, (ev) => {
      fromEvent = true
      apply(ev.data)
    })
    void reload()
  }
  return { items, loaded, failed, unread: computed(() => items.value.filter((n) => !n.Read).length), reload, markRead, markAllRead }
}

async function reload() {
  fromEvent = false
  try {
    const v = await InboxService.Notifications()
    if (!fromEvent) apply(v)
  } catch (e) {
    if (!fromEvent) failed.value = e instanceof Error ? e.message : String(e)
  }
}

/** Marks one read here at once, then on the Go side (its event confirms). */
async function markRead(id: string) {
  const n = items.value.find((x) => x.ID === id)
  if (!n || n.Read) return
  n.Read = true
  await InboxService.MarkRead(id).catch(() => void reload())
}

async function markAllRead() {
  for (const n of items.value) n.Read = true
  await InboxService.MarkAllRead().catch(() => void reload())
}

/** Only for tests: forget the shared state. */
export function resetNotifications() {
  items.value = []
  loaded.value = false
  failed.value = null
  started = false
  fromEvent = false
}

/** When it happened: "just now", "5 min ago", "3 h ago", then "Yesterday 21:40". */
export function noticeTime(atMs: number, nowMs: number): string {
  const min = Math.floor((nowMs - atMs) / 60_000)
  if (min < 1) return 'just now'
  if (min < 60) return `${min} min ago`
  if (min < 6 * 60) return `${Math.floor(min / 60)} h ago`
  return historyTime(atMs, nowMs)
}

/** Where a notification leads: its config's page on that process, else Issues. */
export function noticeRoute(n: NotificationView): RouteLocationRaw {
  if (!n.Config) return '/issues'
  return configRoute(n.Config, n.Process ? { process: n.Process } : {})
}

/** The word for how serious a notification is ("error", "warning", "info"). */
export function severityWord(severity: string): string {
  if (severity === 'error') return 'Error'
  if (severity === 'warning') return 'Warning'
  return 'Info'
}
