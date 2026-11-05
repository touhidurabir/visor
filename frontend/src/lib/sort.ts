// Flow 1 of the polish batch: on the dashboard, a config's failing
// processes come first, so a problem is never below the fold of a long
// config. "Failing" is the Go side's classification (lib/health.ts
// isFailing). The rest keep the order Go sent them in (the sort is stable).
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { isFailing } from './health'

/** The processes with the failing ones first, each part in its original order. */
export function failingFirst(procs: ProcessView[]): ProcessView[] {
  return [...procs.filter(isFailing), ...procs.filter((p) => !isFailing(p))]
}

/** Each config with its processes in failingFirst order; configs keep their order. */
export function sortFailingFirst(configs: ConfigView[]): ConfigView[] {
  return configs.map((c) => (c.Processes?.some(isFailing) ? { ...c, Processes: failingFirst(c.Processes) } : c))
}
