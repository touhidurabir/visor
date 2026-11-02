// The state behind "Add new": one draft shared by both pages, the live
// checks on it, and the save. Pure draft logic is in draft.ts.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ConfigService, type ApplyResultView, type ExplainView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { Check } from '../../bindings/github.com/touhidurabir/visor/internal/validate'
import { createDraft, type TemplateInfo } from './draft'
import { describeError } from './faults'
import { saveErrorText } from './config-errors'
import { showToast } from './toast'

const CHECK_DELAY_MS = 300

export const draft = createDraft({
  render: (name, values) => ConfigService.RenderProgram(name, values),
  parse: (content) => ConfigService.ProgramValues(content),
})

const templates = ref<TemplateInfo[]>([])
const dir = ref('')
const ready = ref(false)
const loadError = ref<string | null>(null)

const checks = ref<Check[] | null>(null)
/** The same text explained line by line (the same checks, with meanings and fixes). */
const explained = ref<ExplainView | null>(null)
const checkFailed = ref<string | null>(null)
/** The text and file name the checks above were run on. */
const checked = ref<{ fileName: string; content: string } | null>(null)

const busy = ref(false)
const failure = ref<string | null>(null)
const created = ref<{ path: string; fileName: string; res: ApplyResultView } | null>(null)

let starting: Promise<void> | null = null

/** Loads the templates and the include folder once, and starts from the first template. */
function start(): Promise<void> {
  starting ??= (async () => {
    try {
      const [ts, d, ext] = await Promise.all([ConfigService.Templates(), ConfigService.IncludeDir(), ConfigService.IncludeExt()])
      templates.value = (ts ?? []) as TemplateInfo[]
      dir.value = d
      draft.setExt(ext || '.ini')
      if (!draft.state.content && !draft.state.name && templates.value.length) await draft.applyTemplate(templates.value[0])
      ready.value = true
    } catch (e) {
      loadError.value = describeError(e)
      starting = null // try again on the next visit
    }
  })()
  return starting
}

let timer: ReturnType<typeof setTimeout> | undefined
let seq = 0

async function runChecks() {
  clearTimeout(timer)
  const mine = ++seq
  const snap = { fileName: draft.state.fileName, content: draft.state.content }
  try {
    const [cs, ex] = await Promise.all([
      ConfigService.Validate(snap.fileName, snap.content, ''),
      ConfigService.Explain(snap.fileName, snap.content, ''),
    ])
    if (mine !== seq) return
    checks.value = cs ?? []
    explained.value = ex
    checkFailed.value = null
    checked.value = snap
  } catch (e) {
    if (mine === seq) checkFailed.value = describeError(e)
  }
}

/** Joins the include folder and a file name the way Go does. */
function pathOf(fileName: string): string {
  return dir.value.replace(/\/+$/, '') + '/' + fileName
}

export function useNewConfig() {
  void start()

  // Checks follow the text and the file name, a moment after typing stops.
  watch(
    () => [draft.state.content, draft.state.fileName, ready.value] as const,
    () => {
      if (!ready.value) return
      clearTimeout(timer)
      timer = setTimeout(() => void runChecks(), CHECK_DELAY_MS)
    },
    { immediate: true },
  )
  // Editing after a save makes the saved result stale.
  watch(
    () => [draft.state.content, draft.state.fileName],
    () => {
      failure.value = null
      created.value = null
    },
  )
  onBeforeUnmount(() => clearTimeout(timer))

  const upToDate = computed(
    () => !!checked.value && checked.value.content === draft.state.content && checked.value.fileName === draft.state.fileName,
  )
  const errorCount = computed(() => (checks.value ?? []).filter((c) => c.Level === 'error').length)
  /** The checks are about the text on screen and found errors. */
  const hasErrors = computed(() => upToDate.value && errorCount.value > 0)

  async function save() {
    if (busy.value) return
    busy.value = true
    failure.value = null
    created.value = null
    try {
      await runChecks()
      if (errorCount.value) {
        showToast(`Fix ${errorCount.value === 1 ? 'the error' : 'the errors'} in the checks first.`, 'error')
        return
      }
      const fileName = draft.state.fileName
      const res = await ConfigService.Create(fileName, draft.state.content)
      created.value = { path: pathOf(fileName), fileName, res }
      showToast(res.Errors?.length ? `Saved ${fileName}, but ${res.Errors.length} could not apply.` : `Saved and applied ${fileName}.`, res.Errors?.length ? 'error' : 'info')
    } catch (e) {
      failure.value = saveErrorText(e, draft.state.fileName)
    } finally {
      busy.value = false
    }
  }

  return { draft, templates, dir, ready, loadError, checks, explained, checked, checkFailed, upToDate, errorCount, hasErrors, busy, failure, created, save, retry: () => void start() }
}

/** Forgets the draft and everything about it (Cancel, or after "Open it"). */
export function resetNewConfig() {
  draft.reset()
  checks.value = null
  explained.value = null
  checked.value = null
  failure.value = null
  created.value = null
  starting = null
  ready.value = false
}
