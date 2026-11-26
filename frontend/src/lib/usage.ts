// CPU and memory per process (M10): the figures and the SVG paths of the
// dashboard's sparkline and the config page's charts. -1 means unknown.

/** "4.1 %", "120 %" (100 % is one core), "–" when unknown. */
export function fmtCpu(v: number): string {
  if (v < 0) return '–'
  if (v < 0.05) return '0 %'
  return `${v >= 99.95 ? Math.round(v) : v.toFixed(1)} %`
}

/** "980 KB", "182 MB", "1.2 GB" in 1024 steps, "–" when unknown. */
export function fmtBytes(n: number): string {
  if (n < 0) return '–'
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n / 1024
  let i = 0
  // Compare as shown: 1023.7 MB reads 1.0 GB, not 1024 MB.
  while (Math.round(v) >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 9.95 || v === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

/** The dashboard's figures: CPU reads "…" until the second sample. */
export function usageLabel(p: { CPU: number; MemBytes: number }): { cpu: string; mem: string; known: boolean } {
  const known = p.MemBytes >= 0
  return { cpu: known && p.CPU < 0 ? '…' : fmtCpu(p.CPU), mem: fmtBytes(p.MemBytes), known }
}

const r = (n: number) => Math.round(n * 10) / 10

/** A polyline's points for values in a w×h box, the peak at the top. */
export function sparkPath(values: number[], w: number, h: number): string {
  if (values.length < 2) return ''
  const top = Math.max(...values, 1)
  return values.map((v, i) => `${r((i * w) / (values.length - 1))},${r(h - 1 - (v / top) * (h - 2))}`).join(' ')
}

/** Samples further apart than this are drawn as a gap (a missed sample). */
const GAP_MS = 25_000

/** Polylines for points between fromMs and toMs scaled to max, split where
 *  a value is unknown or samples are missing. */
export function chartPaths(points: { AtMs: number; v: number }[], fromMs: number, toMs: number, max: number, w: number, h: number): string[] {
  const out: string[] = []
  let cur: string[] = []
  let last = -Infinity
  const span = Math.max(toMs - fromMs, 1)
  const top = Math.max(max, 1e-9)
  for (const p of points) {
    if (p.AtMs < fromMs) continue
    if (p.v < 0 || p.AtMs - last > GAP_MS) {
      if (cur.length) out.push(cur.join(' '))
      cur = []
    }
    last = p.AtMs
    if (p.v < 0) continue
    cur.push(`${r(((p.AtMs - fromMs) / span) * w)},${r(h - (Math.min(p.v, top) / top) * h)}`)
  }
  if (cur.length) out.push(cur.join(' '))
  return out
}

type UsageRow = { CPU: number; MemBytes: number; Procs: number; PID: number; Unmeasurable: boolean }

/** The cell's mark when there are no figures: "…" while a running process
 *  waits for its first sample, "–" when it isn't running or can't be measured. */
export function usageMark(p: Pick<UsageRow, 'CPU' | 'MemBytes' | 'PID' | 'Unmeasurable'>): string {
  return p.PID > 0 && !p.Unmeasurable ? '…' : '–'
}

/** What the Usage cell says to a screen reader and in its tooltip. */
export function usageText(p: UsageRow): string {
  const u = usageLabel(p)
  if (!u.known) {
    if (p.PID <= 0) return 'Not running'
    return p.Unmeasurable ? 'Visor can’t measure this process: another user’s, or hidden from this user on the server' : 'Measuring CPU and memory'
  }
  const kids = p.Procs > 1 ? `, with ${p.Procs - 1} ${p.Procs === 2 ? 'child' : 'children'}` : ''
  return `CPU ${u.cpu === '…' ? 'measuring' : u.cpu}, memory ${u.mem}${kids}`
}
