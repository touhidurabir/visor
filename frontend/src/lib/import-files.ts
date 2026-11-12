// The state behind Add new → Import files: config files picked or dropped
// from elsewhere (a repo, a file an assistant wrote), each with the name it
// gets in the include folder, checked together as you edit, then added in
// one go. The originals are only read. Pure helpers are exported for tests.
import { computed, reactive, ref, watch } from 'vue'
import { Events } from '@wailsio/runtime'
import { ConfigService, type DroppedFileView, type ExplainView, type ImportResultView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { VisorFilesDropped } from '../../bindings/events'
import { showToast } from './toast'
import { describeError } from './faults'

const CHECK_DELAY_MS = 300
/** Larger than this is not a program config; Go refuses it too. */
export const MAX_IMPORT_BYTES = 1 << 20
/** Extensions a config may already carry, swapped for the include setting's. */
const CONFIG_EXTS = ['.ini', '.conf', '.cfg', '.txt']

export interface ImportFile {
  id: number
  /** The picked file's own name. */
  source: string
  /** The name it gets in the include folder. */
  fileName: string
  content: string
  /** Add it (unticked files are left out of the checks and the import). */
  include: boolean
  /** The editor is open. */
  open: boolean
  /** Why it couldn't be read ("" when it was). */
  readError: string
}

/**
 * The name a file gets in the include folder: its own name, with the
 * include setting's extension (ext, e.g. ".ini") in place of a config
 * extension it has, or added when it has none. A file supervisord would
 * otherwise skip gets a name it loads.
 */
export function targetName(source: string, ext: string): string {
  const name = source.trim()
  if (!ext || name.endsWith(ext)) return name
  // supervisord's include glob is case-sensitive: web.INI isn't *.ini.
  const lower = name.toLowerCase()
  const known = CONFIG_EXTS.find((e) => lower.endsWith(e))
  return (known ? name.slice(0, -known.length) : name) + ext
}

/** Visor takes at most this many files in one import (Go refuses more). */
export const MAX_IMPORT_FILES = 50

/** The program names an explained file defines. */
export function programsOf(v: ExplainView | null | undefined): string[] {
  return (v?.Sections ?? []).map((s) => s.Program).filter((p) => !!p)
}

/** "Add 1 config", "Add 3 configs". */
export function addLabel(n: number): string {
  if (!n) return 'Add configs'
  return `Add ${n} ${n === 1 ? 'config' : 'configs'}`
}

/** One file's result as one sentence. */
export function resultLine(r: ImportResultView): { ok: boolean; text: string } {
  if (r.Error) return { ok: false, text: r.Written ? `Saved, but: ${r.Error}` : r.Error }
  const added = r.Result.Added ?? []
  const problems = r.Result.Errors ?? []
  if (problems.length) return { ok: false, text: `Saved, but supervisord reported: ${problems.join('; ')}` }
  const what = added.length ? `supervisord added ${added.join(', ')}` : 'supervisord already knew it'
  if (r.StartError) return { ok: false, text: `${what}, but starting failed: ${r.StartError}` }
  return { ok: true, text: r.Started ? `${what} and started it.` : `${what}.` }
}

let nextId = 1
const files = reactive<ImportFile[]>([])
const views = ref<Record<number, ExplainView>>({})
const checkFailed = ref<string | null>(null)
/** What the views above were checked on: id, name and content of each included file. */
const checkedKey = ref('')
const ext = ref('')
const dir = ref('')
const ready = ref(false)
const startAfter = ref(true)
const busy = ref(false)
const failure = ref<string | null>(null)
const results = ref<ImportResultView[] | null>(null)

const included = computed(() => files.filter((f) => f.include && !f.readError))
const keyOf = (fs: ImportFile[]) => JSON.stringify(fs.map((f) => [f.id, f.fileName, f.content]))

let starting: Promise<void> | null = null
function start(): Promise<void> {
  starting ??= (async () => {
    try {
      const [d, e] = await Promise.all([ConfigService.IncludeDir(), ConfigService.IncludeExt()])
      dir.value = d
      ext.value = e // "" for an include setting like *, which loads any name
      ready.value = true
    } catch (e) {
      failure.value = describeError(e)
      starting = null
    }
  })()
  return starting
}

let timer: ReturnType<typeof setTimeout> | undefined
let seq = 0

async function runChecks() {
  clearTimeout(timer)
  const mine = ++seq
  const batch = included.value.map((f) => ({ id: f.id, fileName: f.fileName, content: f.content }))
  const key = keyOf(included.value)
  if (!batch.length) {
    views.value = {}
    checkedKey.value = key
    return
  }
  try {
    const out = (await ConfigService.CheckImport(batch.map((b) => ({ FileName: b.fileName, Content: b.content })))) ?? []
    if (mine !== seq) return
    const next: Record<number, ExplainView> = {}
    batch.forEach((b, i) => {
      if (out[i]) next[b.id] = out[i]
    })
    views.value = next
    checkFailed.value = null
    checkedKey.value = key
  } catch (e) {
    if (mine === seq) checkFailed.value = describeError(e)
  }
}

watch(
  () => keyOf(included.value),
  () => {
    failure.value = null
    clearTimeout(timer)
    timer = setTimeout(() => void runChecks(), CHECK_DELAY_MS)
  },
)

/** Reads picked or dropped files into the list (the originals are never changed). */
/** A file to add: its name and text, or why it can't be read. */
interface Picked {
  name: string
  read: () => Promise<{ content: string; readError: string }>
}

/** Adds files to the list; returns how many were left out past MAX_IMPORT_FILES. */
async function addPicked(list: Picked[]): Promise<number> {
  await start()
  results.value = null
  let skipped = 0
  for (const p of list) {
    if (files.length >= MAX_IMPORT_FILES) {
      skipped++
      continue
    }
    const { content, readError } = await p.read()
    files.push({ id: nextId++, source: p.name, fileName: targetName(p.name, ext.value), content, include: !readError, open: false, readError })
  }
  // A single file opens straight in the editor.
  if (files.length === 1 && !files[0].readError) files[0].open = true
  return skipped
}

/** Reads picked files (the page's file picker, or a drop in the server build) into the list. */
export function addFiles(list: Iterable<File>): Promise<number> {
  return addPicked(
    Array.from(list, (f) => ({
      name: f.name,
      read: async () => {
        if (f.size > MAX_IMPORT_BYTES) return { content: '', readError: 'Larger than 1 MB, too large for a program config.' }
        try {
          return { content: await f.text(), readError: '' }
        } catch (e) {
          return { content: '', readError: `Couldn’t read it: ${describeError(e)}` }
        }
      },
    })),
  )
}

/** Adds files dropped on the window, which Go has read (app.ReadDropped). */
export function addDropped(list: DroppedFileView[]): Promise<number> {
  return addPicked(list.map((d) => ({ name: d.Name, read: async () => ({ content: d.Content, readError: d.Error }) })))
}

/**
 * The desktop app takes file drops natively (Wails EnableFileDrop): files
 * dropped anywhere on the main window arrive as visor:files-dropped, read
 * by Go, and open in Add new → Import files.
 */
export function followDrops(router: { push: (to: string) => unknown }) {
  Events.On(VisorFilesDropped, (ev: { data: DroppedFileView[] | null }) => {
    void addDropped(ev.data ?? []).then((skipped) => {
      if (skipped) showToast(`Visor imports at most ${MAX_IMPORT_FILES} files at a time; ${skipped === 1 ? 'one was' : `${skipped} were`} left out.`, 'error')
    })
    void router.push('/new/import')
  })
}

/** True in the desktop app, where drops come natively instead of to the page. */
export function nativeDrops(): boolean {
  return (window as unknown as { _wails?: { flags?: { enableFileDrop?: boolean } } })._wails?.flags?.enableFileDrop === true
}

export function removeFile(id: number) {
  const i = files.findIndex((f) => f.id === id)
  if (i >= 0) files.splice(i, 1)
}

/** Forgets every file and result (Cancel, Import more). */
export function resetImport() {
  files.splice(0)
  views.value = {}
  checkedKey.value = ''
  results.value = null
  failure.value = null
  checkFailed.value = null
}

export function useImport() {
  void start()
  const upToDate = computed(() => checkedKey.value === keyOf(included.value))
  /** Included files with a problem that blocks adding. */
  const blocked = computed(() => included.value.filter((f) => (views.value[f.id]?.SaveErrors ?? 0) > 0))

  async function importAll() {
    if (busy.value || !included.value.length) return
    busy.value = true
    failure.value = null
    try {
      await runChecks()
      if (blocked.value.length) {
        failure.value = `Fix ${blocked.value.map((f) => f.fileName).join(', ')} first.`
        return
      }
      const batch = included.value.map((f) => ({ FileName: f.fileName, Content: f.content }))
      const res = (await ConfigService.Import(batch, startAfter.value)) ?? []
      // Files now in the include folder leave the list (by position: the
      // fields are locked while adding); the rest stay to fix and try again.
      const sent = included.value.map((f) => f.id)
      res.forEach((r, i) => {
        if (r.Written) removeFile(sent[i])
      })
      results.value = res
    } catch (e) {
      failure.value = describeError(e)
    } finally {
      busy.value = false
    }
  }

  return { files, views, dir, ext, ready, included, upToDate, blocked, checkFailed, startAfter, busy, failure, results, importAll, recheck: () => void runChecks() }
}
