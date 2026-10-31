import { describe, expect, it } from 'vitest'
import { filterConfigs } from './filter'
import type { ConfigView, ProcessView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

const p = (FullName: string) => ({ FullName, Name: FullName }) as ProcessView
const cfg = (FileName: string, names: string[]) =>
  ({ FileName, Path: '/x/' + FileName, Processes: names.map(p) }) as ConfigView
const configs = [cfg('shop-api.conf', ['shop:queue_00', 'shop:queue_01', 'scheduler']), cfg('mailer.conf', ['digest'])]

describe('filterConfigs', () => {
  it('returns everything for an empty query', () => {
    expect(filterConfigs(configs, '  ')).toBe(configs)
  })
  it('keeps only matching processes, case-insensitively', () => {
    const out = filterConfigs(configs, 'QUEUE_01')
    expect(out).toHaveLength(1)
    expect(out[0].Processes?.map((x) => x.FullName)).toEqual(['shop:queue_01'])
  })
  it('keeps all processes when the file name matches', () => {
    const out = filterConfigs(configs, 'mailer')
    expect(out).toHaveLength(1)
    expect(out[0].Processes).toHaveLength(1)
    expect(filterConfigs(configs, 'shop-api')[0].Processes).toHaveLength(3)
  })
  it('drops configs with no match', () => {
    expect(filterConfigs(configs, 'zzz')).toEqual([])
  })
  it('handles configs with no processes', () => {
    const empty = { FileName: 'e.conf', Path: '/e', Processes: null } as ConfigView
    expect(filterConfigs([empty], 'e.conf')).toEqual([empty])
  })
})
