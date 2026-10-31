// Display helpers shared by views.
import type { ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

/**
 * formatDuration uses the two largest units, like the Go side's labels:
 * "59s", "41m", "3h 2m", "6d 3h", "5d".
 */
export function formatDuration(ms: number): string {
  const total = Math.floor(Math.max(ms, 0) / 1000)
  const days = Math.floor(total / 86400)
  const hours = Math.floor(total / 3600) % 24
  const mins = Math.floor(total / 60) % 60
  const secs = total % 60
  const pair = (a: number, au: string, b: number, bu: string) => (b === 0 ? `${a}${au}` : `${a}${au} ${b}${bu}`)
  if (days > 0) return pair(days, 'd', hours, 'h')
  if (hours > 0) return pair(hours, 'h', mins, 'm')
  if (mins > 0) return `${mins}m`
  return `${secs}s`
}

/**
 * shortSince trims a ProcessView.Since label for compact lists: running
 * processes drop "up"; "failing …" and "stopped …" keep their word so
 * state never relies on colour alone.
 */
export function shortSince(since: string): string {
  return since.replace(/^up /, '')
}

/**
 * duration is the human label for an elapsed time: "41m", "3h 2m", "6d 3h";
 * under a minute it is "just now".
 */
export function duration(ms: number): string {
  return ms < 60_000 ? 'just now' : formatDuration(ms)
}

/**
 * sinceLabel re-computes ProcessView.Since against the browser clock so rows
 * keep ticking between overview events. It falls back to the label the Go
 * side sent when the process has no usable timestamp.
 */
export function sinceLabel(p: ProcessView, nowMs: number): string {
  if (p.Status === 'running' && p.StartMs > 0) return `up ${duration(nowMs - p.StartMs)}`
  if ((p.Status === 'failing' || p.Status === 'stopped') && p.StopMs > 0) {
    return `${p.Status} ${duration(nowMs - p.StopMs)}`
  }
  return p.Since
}
