import type { ConfigView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

/**
 * filterConfigs narrows the dashboard table. A config whose file name matches
 * is kept with all its processes; otherwise only matching processes stay, and
 * a config with none is dropped. The match is case-insensitive.
 */
export function filterConfigs(configs: ConfigView[], query: string): ConfigView[] {
  const q = query.trim().toLowerCase()
  if (!q) return configs
  const out: ConfigView[] = []
  for (const c of configs) {
    if (c.FileName.toLowerCase().includes(q)) {
      out.push(c)
      continue
    }
    const procs = (c.Processes ?? []).filter(
      (p) => p.FullName.toLowerCase().includes(q) || p.Name.toLowerCase().includes(q),
    )
    if (procs.length) out.push({ ...c, Processes: procs })
  }
  return out
}
