// The new config being written, shared by "Write it" and "Fill a form" so
// switching between them keeps everything. The text (`content`) is what gets
// saved. The form edits `name` and `values` and re-renders the text; going
// from the text back to the form parses it again.
//
// Rendering and parsing are done by Go (config.RenderProgram and
// config.ProgramValues), passed in as a `Codec` so this stays testable.
import { reactive } from 'vue'

export interface Codec {
  render(name: string, values: Record<string, string>): PromiseLike<string>
  parse(content: string): PromiseLike<{ Name: string; Values: Record<string, string | undefined> | null }>
}

export interface TemplateInfo {
  ID: string
  Label: string
  Values: Record<string, string>
}

export interface DraftState {
  fileName: string
  /** True once the person typed a file name; until then it follows the program name. */
  fileNameEdited: boolean
  content: string
  name: string
  values: Record<string, string>
  templateId: string
  ext: string
}

/** The name of the first [program:x] in text, for the default file name. */
export function firstProgramName(text: string): string {
  return /^\s*\[program:([^\]\r\n]+)\]/m.exec(text)?.[1].trim() ?? ''
}

/** `<name>` plus the include pattern's extension; "new" while there is no name. */
export function defaultFileName(name: string, ext: string): string {
  return (name.trim() || 'new') + ext
}

export function createDraft(codec: Codec) {
  const state = reactive<DraftState>({
    fileName: '',
    fileNameEdited: false,
    content: '',
    name: '',
    values: {},
    templateId: '',
    ext: '.ini',
  })
  // Edits are rendered one after another, so a slow render never overwrites a newer one.
  let renderSeq = 0

  function followName() {
    if (!state.fileNameEdited) state.fileName = defaultFileName(state.name, state.ext)
  }

  async function render() {
    const mine = ++renderSeq
    const text = await codec.render(state.name, state.values)
    if (mine === renderSeq) state.content = text
  }

  return {
    state,

    /** Sets the extension new file names get (from the include pattern). */
    setExt(ext: string) {
      state.ext = ext
      followName()
    },

    /** Starts from a template: its name, values and text. */
    async applyTemplate(t: TemplateInfo) {
      const { name = '', ...values } = t.Values
      state.templateId = t.ID
      state.name = name
      state.values = values
      followName()
      await render()
    },

    /** A form field changed ("name" is the program name). */
    async setField(key: string, value: string) {
      if (key === 'name') {
        state.name = value
        followName()
      } else {
        state.values = { ...state.values, [key]: value }
      }
      await render()
    },

    /** The text was edited in "Write it". */
    setContent(text: string) {
      state.content = text
      renderSeq++ // a render still on its way is stale
      const n = firstProgramName(text)
      if (n) {
        state.name = n
        followName()
      }
    },

    setFileName(n: string) {
      state.fileName = n
      state.fileNameEdited = n.trim() !== ''
      if (!state.fileNameEdited) followName()
    },

    /**
     * Fills the form from the text. Returns why that was not possible (the
     * form then keeps what it had), or null.
     */
    async toForm(): Promise<string | null> {
      try {
        const p = await codec.parse(state.content)
        state.name = p.Name
        state.values = Object.fromEntries(Object.entries(p.Values ?? {}).filter((kv): kv is [string, string] => kv[1] !== undefined))
        followName()
        return null
      } catch (e) {
        return e instanceof Error ? e.message : String(e)
      }
    },

    /** Forgets everything; the next visit starts from a template again. */
    reset() {
      renderSeq++
      state.fileName = ''
      state.fileNameEdited = false
      state.content = ''
      state.name = ''
      state.values = {}
      state.templateId = ''
    },
  }
}

export type Draft = ReturnType<typeof createDraft>
