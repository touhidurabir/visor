import { describe, expect, it } from 'vitest'
import { classifySaveError, saveErrorText } from './config-errors'

describe('classifySaveError', () => {
  it('recognises Go’s messages', () => {
    expect(classifySaveError(new Error('/x/a.ini: file changed on disk'))).toBe('changed')
    expect(classifySaveError(new Error('/x/a.ini was deleted: file changed on disk'))).toBe('changed')
    expect(classifySaveError(new Error('cannot write /x/a.ini: config folder is not writable'))).toBe('readonly')
    expect(classifySaveError(new Error('config has errors: line 3: Unknown key'))).toBe('invalid')
    expect(classifySaveError(new Error('saved a.ini, but applying it failed: nope'))).toBe('other')
  })

  it('writes a sentence for each', () => {
    expect(saveErrorText(new Error('x: file changed on disk'), 'a.ini')).toMatch(/changed on disk/)
    expect(saveErrorText(new Error('config has errors: line 3: Unknown key'), 'a.ini')).toBe('Fix this first: line 3: Unknown key')
    expect(saveErrorText(new Error('boom'), 'a.ini')).toBe('boom')
  })
})
