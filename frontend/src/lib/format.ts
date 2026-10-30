// Display helpers shared by views.

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
