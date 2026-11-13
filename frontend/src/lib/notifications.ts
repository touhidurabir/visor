// Visor's own notification list (the bell): one shared copy, loaded once
// and kept current by visor:notifications events. Banners from macOS are
// extra; everything Visor notifies about lands here with read/unread state.
import { computed, ref, watch } from 'vue'
import { Events } from '@wailsio/runtime'
import type { RouteLocationRaw } from 'vue-router'
import { type NotificationView, type NotificationsView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { InboxService } from './api'
import { VisorNotifications } from '../../bindings/events'
import { historyTime } from './format'
import { configRoute } from './links'
import { issueKind } from './issue-status'
import { currentTab, LOCAL } from './tabs'

// Each tab has its own list; the bell shows the tab on screen.
const items = ref<NotificationView[]>([])
const loaded = ref(false)
const failed = ref<string | null>(null)
let started = false
let fromEvent = false
const byTab = new Map<string, NotificationView[]>()

function apply(v: NotificationsView) {
  byTab.set(v.Server || LOCAL, v.Items ?? [])
  if ((v.Server || LOCAL) !== currentTab.value) return
  items.value = v.Items ?? []
  loaded.value = true
  failed.value = null
}

/** Starts following the list (once per window); later calls share it. */
export function useNotifications() {
  if (!started) {
    started = true
    Events.On(VisorNotifications, (ev) => {
      if ((ev.data?.Server || LOCAL) === currentTab.value) fromEvent = true
      apply(ev.data)
    })
    watch(currentTab, (id) => {
      items.value = byTab.get(id) ?? []
      void reload()
    })
    void reload()
  }
  return { items, loaded, failed, unread: computed(() => items.value.filter((n) => !n.Read).length), reload, markRead, markAllRead }
}

async function reload() {
  fromEvent = false
  const id = currentTab.value
  try {
    const v = await InboxService.Notifications()
    if (!fromEvent && id === currentTab.value) apply({ ...v, Server: v.Server || id })
  } catch (e) {
    if (!fromEvent && id === currentTab.value) failed.value = e instanceof Error ? e.message : String(e)
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
  byTab.clear()
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

export type SeverityLevel = 'error' | 'warning' | 'info'

/** severityLevel is a notification's Severity, with anything unknown as info. */
export function severityLevel(severity: string): SeverityLevel {
  return severity === 'error' || severity === 'warning' ? severity : 'info'
}

/** The word for how serious a notification is ("error", "warning", "info"). */
export function severityWord(severity: string): string {
  return { error: 'Error', warning: 'Warning', info: 'Info' }[severityLevel(severity)]
}

/** A check mark, for good news (supervisord answering again). */
const OK_ICON = 'M5 12.5l4.5 4.5L19 7.5'

/**
 * noticeIcon is the kind icon (an SVG path) a notification leads with: exit,
 * can't start, crash loop or supervisord down; good news gets a check.
 */
export function noticeIcon(n: Pick<NotificationView, 'Kind' | 'Severity'>): string {
  return severityLevel(n.Severity) === 'info' ? OK_ICON : issueKind(n.Kind).icon
}

export interface NoticeGroup {
  name: 'Today' | 'Earlier'
  items: NotificationView[]
}

/**
 * groupByDay splits the list (newest first) into Today, since local
 * midnight, and Earlier, keeping the order; an empty group is left out.
 * Anything stamped after nowMs (a clock change) counts as today.
 */
export function groupByDay(items: NotificationView[], nowMs: number): NoticeGroup[] {
  const now = new Date(nowMs)
  const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const today = items.filter((n) => n.AtMs >= midnight)
  const earlier = items.filter((n) => n.AtMs < midnight)
  const groups: NoticeGroup[] = [
    { name: 'Today', items: today },
    { name: 'Earlier', items: earlier },
  ]
  return groups.filter((g) => g.items.length > 0)
}
