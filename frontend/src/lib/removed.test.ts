import { describe, expect, it } from 'vitest'
import { removedRows } from './removed'

describe('removedRows', () => {
  it('labels each removed file with its folder and when it went', () => {
    const now = new Date(2026, 10, 1, 12, 0).getTime()
    const at = new Date(2026, 10, 1, 9, 5).toISOString()
    expect(removedRows([{ Path: '/conf.d/web.ini', FileName: 'web.ini', BackupID: 'b1', At: at }], now)).toEqual([
      { path: '/conf.d/web.ini', fileName: 'web.ini', backupID: 'b1', folder: '/conf.d', when: 'Removed Today 09:05' },
    ])
  })
  it('is empty without a list', () => {
    expect(removedRows(null, 0)).toEqual([])
  })
})
