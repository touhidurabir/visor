<script setup lang="ts">
// Doctor (spec §5.1, board 1 · First launch / setup): the five setup checks
// beside the lens, and the fix for the first one that fails. On first launch
// (firstRun) it fills the window until setup is finished; it is also the
// Doctor page in the sidebar. Visor never runs sudo or installs anything:
// every fix is a command to copy or a file to choose.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { DoctorService, SettingsService, type DoctorView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import Lens from '../components/Lens.vue'
import { copyText } from '../lib/clipboard'
import { chownCommand, installCommands, lensText, mkdirCommand, rootFixes, situationCopy, startCommand, startTips } from '../lib/doctor'
import { describeError } from '../lib/faults'
import { loginLocked, useLaunchAtLogin } from '../lib/login-item'
import { showToast } from '../lib/toast'

const props = defineProps<{ firstRun?: boolean }>()
const emit = defineEmits<{
  /** First-run setup is finished: show the app. */
  done: []
  /** Leave first-run Doctor for a page (setup isn't finished). */
  leave: [route: string]
}>()

const router = useRouter()
const view = ref<DoctorView | null>(null)
const loadError = ref('')
const checking = ref(false)
const runs = ref(0) // re-keys the lens so it plays again after Check again

// The config chooser (No config, and "It's installed somewhere else…").
const pathDraft = ref('')
const pathError = ref('')
const saving = ref(false)
const chooserOpen = ref(false)
const rootHelpOpen = ref(false)

// First-run toggles; both on by default (spec §5.1).
const login = useLaunchAtLogin()
const loginOn = ref(true)
const notifyOn = ref(true)
const finishing = ref(false)

const copied = ref('')

const situation = computed(() => view.value?.Situation ?? 'ready')
const words = computed(() => situationCopy(situation.value))
const lens = computed(() => lensText(view.value ?? { Steps: [] }))
const steps = computed(() => view.value?.Steps ?? [])
const segments = computed(() => steps.value.map((s) => s.Status))
const notMoved = computed(() => !!view.value && !view.value.Moved)
const loginLockedNow = computed(() => notMoved.value || loginLocked(login.view.value?.Status ?? ''))
const loginLabel = computed(() => {
  if (notMoved.value) return 'Open Visor when I log in (after you move it)'
  if (login.view.value?.Status === 'unsupported') return 'Open Visor when I log in (in the installed Visor.app)'
  return 'Open Visor when I log in'
})
const fixes = computed(() => rootFixes(view.value?.User ?? ''))
// The folder fix: create it when it's missing, else make it yours.
const folderFix = computed(() => {
  const dir = view.value?.IncludeDir ?? ''
  return view.value?.IncludeMissing
    ? { label: 'Create the folder your programs go in', command: mkdirCommand(dir) }
    : { label: 'To add and edit configs, make the folder yours', command: chownCommand(dir) }
})
const showChooser = computed(() => situation.value === 'noconf' || (situation.value === 'nosup' && chooserOpen.value))

const glyph: Record<string, string> = { ok: '✓', bad: '!', wait: '' }
const spoken: Record<string, string> = { ok: 'done', bad: 'needs attention', wait: 'not checked yet' }

function adopt(v: DoctorView) {
  view.value = v
  runs.value++
  if (!pathDraft.value || v.Overridden) pathDraft.value = v.Overridden ? v.ConfigPath : (v.Looked?.length === 1 ? v.Looked[0] : '')
}

async function run() {
  checking.value = true
  try {
    adopt(await DoctorService.Run())
    loadError.value = ''
  } catch (e) {
    loadError.value = describeError(e)
  } finally {
    checking.value = false
  }
}

onMounted(() => {
  void run()
  if (props.firstRun) void login.load()
})

async function choose() {
  try {
    const path = await SettingsService.ChooseConfig()
    if (path) {
      pathDraft.value = path
      pathError.value = ''
    }
  } catch (e) {
    pathError.value = describeError(e)
  }
}

async function useConfig() {
  saving.value = true
  pathError.value = ''
  try {
    adopt(await DoctorService.UseConfig(pathDraft.value))
    chooserOpen.value = false
  } catch (e) {
    pathError.value = describeError(e)
  } finally {
    saving.value = false
  }
}

// The command to copy when Visor doesn't start it: this config's, else Homebrew's.
const manualStart = computed(() => view.value?.StartCommand || startCommand)
const starting = ref(false)
async function startSupervisord() {
  starting.value = true
  try {
    adopt(await DoctorService.StartSupervisord())
  } catch (e) {
    showToast(describeError(e), 'error')
  } finally {
    starting.value = false
  }
}

async function copy(text: string) {
  try {
    await copyText(text)
    copied.value = text
  } catch (e) {
    showToast(describeError(e), 'error')
  }
}

/** Open the app: on first run this finishes setup with the toggles as left. */
async function openDashboard(withToggles: boolean) {
  if (!props.firstRun) {
    void router.push('/')
    return
  }
  finishing.value = true
  try {
    const warning = await DoctorService.Finish(withToggles && loginOn.value && !loginLockedNow.value, withToggles ? notifyOn.value : (view.value?.Notify ?? true))
    if (warning) showToast(warning, 'error')
    emit('done')
  } catch (e) {
    showToast(describeError(e), 'error')
  } finally {
    finishing.value = false
  }
}

function openSettings() {
  if (props.firstRun) emit('leave', '/settings')
  else void router.push('/settings')
}
</script>

<template>
  <div :class="['doctor', { embedded: !firstRun }]">
    <div v-if="loadError && !view" class="load-error" role="alert">
      <strong>Couldn’t run the checks</strong>
      <p>{{ loadError }}</p>
      <button type="button" class="btn" @click="run">Try again</button>
    </div>

    <div v-else-if="view" class="layout">
      <div class="lens-col">
        <Lens :key="runs" :segments="segments" :big="lens.big" :small="lens.small" />
      </div>

      <div class="text-col" :aria-busy="checking">
        <div v-if="notMoved" class="moved" role="status">
          <span class="bang" aria-hidden="true">!</span>
          <div>
            <strong>Move Visor to Applications first</strong>
            <span>It’s running from the disk image or Downloads, so it can’t open when you log in and macOS may forget your settings for it. Quit Visor, drag it into Applications, and open it from there.</span>
          </div>
        </div>

        <div class="intro">
          <h1 class="display">{{ words.headline }}</h1>
          <p class="lede">{{ words.lede }}</p>
        </div>

        <ol class="steps" aria-label="What Visor checked">
          <li v-for="(st, i) in steps" :key="st.ID" class="step" :data-status="st.Status">
            <span :class="['mark', `m-${st.Status}`, `d-${i}`]" aria-hidden="true">{{ glyph[st.Status] }}</span>
            <div class="step-text">
              <span :class="{ 't-wait': st.Status === 'wait' }" class="step-title">{{ st.Title }}<span class="sr">: {{ spoken[st.Status] }}</span></span>
              <span v-if="st.Detail" class="path">{{ st.Detail }}</span>
            </div>
          </li>
        </ol>

        <!-- All found -->
        <div v-if="situation === 'ready'" class="block">
          <div v-if="firstRun" class="toggles">
            <label :class="['toggle', { off: loginLockedNow }]">
              <input
                type="checkbox"
                :checked="loginOn && !loginLockedNow"
                :disabled="loginLockedNow"
                @change="loginOn = ($event.target as HTMLInputElement).checked"
              />{{ loginLabel }}
            </label>
            <label class="toggle"><input v-model="notifyOn" type="checkbox" />Notify me when a watched process fails</label>
          </div>
          <div class="actions">
            <button type="button" class="btn go" :disabled="finishing" @click="openDashboard(true)">Open dashboard</button>
            <span class="muted">Visor also lives in your menu bar.</span>
          </div>
        </div>

        <!-- No config / installed somewhere else -->
        <div v-if="showChooser" class="block">
          <label for="doctor-conf" class="label">Your supervisord.conf</label>
          <div class="pick">
            <input
              id="doctor-conf"
              v-model.trim="pathDraft"
              class="path-input"
              type="text"
              spellcheck="false"
              autocomplete="off"
              placeholder="/path/to/supervisord.conf"
              :disabled="view.Overridden"
              @keydown.enter="useConfig"
            />
            <button type="button" class="btn" :disabled="view.Overridden" @click="choose">Choose…</button>
          </div>
          <p v-if="view.Overridden" class="muted note">Set by VISOR_CONFIG. Unset it to choose a file here.</p>
          <p v-if="pathError" class="field-error" role="alert">{{ pathError }}</p>
          <details v-if="view.Looked?.length" class="looked">
            <summary>Where Visor looked</summary>
            <div class="path">
              <div v-for="p in view.Looked" :key="p">{{ p }}</div>
            </div>
          </details>
          <div>
            <button type="button" class="btn go" :disabled="saving || view.Overridden || !pathDraft" @click="useConfig">
              {{ saving ? 'Checking…' : 'Use this config' }}
            </button>
          </div>
        </div>

        <!-- Not installed -->
        <div v-if="situation === 'nosup'" class="block">
          <span class="label">Run these in Terminal, then check again</span>
          <div v-for="c in installCommands" :key="c" class="cmd">
            <code>{{ c }}</code>
            <button type="button" class="copy" @click="copy(c)">{{ copied === c ? 'Copied' : 'Copy' }}</button>
          </div>
          <div class="actions">
            <button type="button" class="btn go" :disabled="checking" @click="run">{{ checking ? 'Checking…' : 'Check again' }}</button>
            <button v-if="!chooserOpen" type="button" class="btn sm" @click="chooserOpen = true">It’s installed somewhere else…</button>
          </div>
          <span class="muted">Visor never installs anything or asks for your password.</span>
        </div>

        <!-- Not running -->
        <div v-if="situation === 'down'" class="block">
          <template v-if="view.CanStart">
            <div class="actions first">
              <button type="button" class="btn go" :disabled="starting || checking" @click="startSupervisord">
                {{ starting ? 'Starting…' : 'Start supervisord' }}
              </button>
              <button type="button" class="btn sm" :disabled="starting || checking" @click="run">{{ checking ? 'Checking…' : 'Check again' }}</button>
            </div>
            <span class="muted">Visor runs <code class="inline">{{ view.StartCommand }}</code> as you. It never uses sudo.</span>
          </template>
          <template v-else>
            <span class="label">Start it as your user</span>
            <p v-if="view.Foreground" class="muted note">
              This config keeps supervisord in the foreground (<code class="inline">nodaemon=true</code>), the way launchd runs it, so Visor
              doesn’t start it. Start it the way you set it up, or run this in Terminal:
            </p>
            <div class="cmd">
              <code>{{ manualStart }}</code>
              <button type="button" class="copy" @click="copy(manualStart)">{{ copied === manualStart ? 'Copied' : 'Copy' }}</button>
            </div>
            <div class="actions">
              <button type="button" class="btn go" :disabled="checking" @click="run">{{ checking ? 'Checking…' : 'Check again' }}</button>
            </div>
          </template>

          <div v-if="view.StartFailure" class="failure" role="alert">
            <strong>supervisord didn’t start</strong>
            <p class="muted">Visor ran <code class="inline">{{ view.StartFailure.Command }}</code>.</p>
            <pre v-if="view.StartFailure.Output || view.StartFailure.Error" class="out">{{ [view.StartFailure.Output, view.StartFailure.Error].filter(Boolean).join('\n') }}</pre>
            <template v-if="view.StartFailure.Log">
              <span class="label">The end of its log</span>
              <span class="path">{{ view.StartFailure.LogPath }}</span>
              <pre class="out">{{ view.StartFailure.Log }}</pre>
            </template>
            <ul class="tips">
              <li v-for="t in startTips" :key="t">{{ t }}</li>
            </ul>
          </div>

          <div>
            <button type="button" class="link" :aria-expanded="rootHelpOpen" @click="rootHelpOpen = !rootHelpOpen">supervisord runs as root?</button>
          </div>
        </div>

        <!-- Root-only socket (and "supervisord runs as root?") -->
        <div v-if="situation === 'denied' || (situation === 'down' && rootHelpOpen)" class="block">
          <span class="label">Pick one, then check again</span>
          <details v-for="(f, i) in fixes" :key="f.title" class="fix" :open="i === 0">
            <summary>{{ f.title }}</summary>
            <p>{{ f.why }}</p>
            <div v-for="c in f.commands" :key="c" class="cmd">
              <code class="pre">{{ c }}</code>
              <button type="button" class="copy" @click="copy(c)">{{ copied === c ? 'Copied' : 'Copy' }}</button>
            </div>
          </details>
          <div v-if="situation === 'denied'" class="actions">
            <button type="button" class="btn go" :disabled="checking" @click="run">{{ checking ? 'Checking…' : 'Check again' }}</button>
          </div>
          <span class="muted">Visor never runs sudo or changes these files for you.</span>
        </div>

        <!-- Wrong username or password -->
        <div v-if="situation === 'auth'" class="block">
          <div class="actions">
            <button type="button" class="btn go" @click="openSettings">Open Settings</button>
            <button type="button" class="btn sm" :disabled="checking" @click="run">{{ checking ? 'Checking…' : 'Check again' }}</button>
          </div>
        </div>

        <!-- Can't edit configs -->
        <div v-if="situation === 'notwritable'" class="block">
          <template v-if="folderFix.command">
            <span class="label">{{ folderFix.label }}</span>
            <div class="cmd">
              <code>{{ folderFix.command }}</code>
              <button type="button" class="copy" @click="copy(folderFix.command)">{{ copied === folderFix.command ? 'Copied' : 'Copy' }}</button>
            </div>
          </template>
          <div class="actions">
            <button type="button" class="btn go" :disabled="finishing" @click="openDashboard(false)">Open dashboard anyway</button>
            <button type="button" class="btn sm" :disabled="checking" @click="run">{{ checking ? 'Checking…' : 'Check again' }}</button>
          </div>
          <span class="muted">Everything else works: watching, starting, stopping and logs.</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.doctor {
  --d-bg: #f2f5f9;
  --d-ink: #14202e;
  --d-muted: #55657a;
  --d-line: #dce3ec;
  --d-card: #ffffff;
  --d-border: #cbd5e1;
  --d-hover: #f7f9fc;
  --d-go: #14202e;
  --d-go-hover: #24344a;
  --d-on-go: #ffffff;
  --d-cmd: #14202e;
  --d-cmd-fg: #e9eef5;
  --d-copy: #2a3a52;
  --d-copy-hover: #3a4c66;
  --d-link: #2f4fd8;
  --d-ok-bg: #ddf3e8;
  --d-ok-fg: #16754a;
  --d-bad-bg: #fde4e1;
  --d-bad-fg: #b42318;
  --d-wait-bg: #e6ebf2;
  --d-wait-fg: #55657a;
  --d-warn-bg: #fff4e0;
  --d-warn-line: #f3d19c;
  --d-warn-fg: #6b4a0e;
  --d-error: #b42318;
  --lens-wait: #c9d3df;
  min-height: 100vh;
  box-sizing: border-box;
  padding: 44px 24px 40px;
  background: var(--d-bg);
  color: var(--d-ink);
  font-size: 14px;
}
/* The builder's dark palette (spec §9): same contrast, deep blue-grey. */
:root[data-theme='dark'] .doctor {
  --d-bg: #12161c;
  --d-ink: #e9eef5;
  --d-muted: #9aa8ba;
  --d-line: #2a3442;
  --d-card: #1a2029;
  --d-border: #3a4656;
  --d-hover: #222a35;
  --d-go: #e9eef5;
  --d-go-hover: #ffffff;
  --d-on-go: #14202e;
  --d-cmd: #0b0f14;
  --d-cmd-fg: #e9eef5;
  --d-copy: #2a3a52;
  --d-copy-hover: #3a4c66;
  --d-link: #8fa8ff;
  --d-ok-bg: #12341f;
  --d-ok-fg: #6fd08c;
  --d-bad-bg: #3a1715;
  --d-bad-fg: #ff8a80;
  --d-wait-bg: #232b36;
  --d-wait-fg: #9aa8ba;
  --d-warn-bg: #33270f;
  --d-warn-line: #6b5320;
  --d-warn-fg: #f3d19c;
  --d-error: #ff8a80;
  --lens-wait: #3a4656;
}
/* As a page beside the sidebar: fill the content area, smaller lens. */
.doctor.embedded {
  min-height: 100%;
  margin: -40px -28px;
  padding: 36px 28px 40px;
  --lens-size: 260px;
}
.layout {
  width: 100%;
  max-width: 1080px;
  margin: 0 auto;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 56px;
}
.embedded .layout { gap: 40px; }
.lens-col { flex: 0 1 var(--lens-size, 340px); display: flex; justify-content: center; }
.text-col { flex: 1 1 400px; max-width: 520px; display: flex; flex-direction: column; gap: 26px; }
.display {
  margin: 0;
  font-family: 'Bricolage Grotesque Variable', -apple-system, system-ui, sans-serif;
  font-optical-sizing: auto;
  font-size: 44px;
  line-height: 1.05;
  font-weight: 700;
  letter-spacing: -0.02em;
}
.embedded .display { font-size: 36px; }
.intro { display: flex; flex-direction: column; gap: 10px; }
.lede { margin: 0; max-width: 46ch; font-size: 16px; line-height: 1.5; color: var(--d-muted); }
.muted { color: var(--d-muted); }
.path { font-family: var(--font-mono); font-size: 12.5px; color: var(--d-muted); word-break: break-all; user-select: text; }

.steps { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
.step { position: relative; display: grid; grid-template-columns: 28px 1fr; column-gap: 14px; padding-bottom: 18px; }
.step:last-child { padding-bottom: 0; }
.step::before { content: ''; position: absolute; left: 13px; top: 28px; bottom: 2px; width: 2px; background: var(--d-line); }
.step:last-child::before { display: none; }
.step-text { display: flex; flex-direction: column; gap: 2px; padding-top: 3px; min-width: 0; }
.step-title { font-weight: 600; }
.t-wait { color: var(--d-muted); }
.sr { position: absolute; left: -9999px; }
.mark { width: 28px; height: 28px; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 14px; font-weight: 700; animation: pop 0.35s ease-out both; }
.m-ok { background: var(--d-ok-bg); color: var(--d-ok-fg); }
.m-bad { background: var(--d-bad-bg); color: var(--d-bad-fg); }
.m-wait { background: var(--d-wait-bg); color: var(--d-wait-fg); }
.d-0 { animation-delay: 0.15s; }
.d-1 { animation-delay: 0.5s; }
.d-2 { animation-delay: 0.85s; }
.d-3 { animation-delay: 1.2s; }
.d-4 { animation-delay: 1.55s; }
@keyframes pop { 0% { transform: scale(0.6); opacity: 0; } 70% { transform: scale(1.08); } 100% { transform: scale(1); opacity: 1; } }

.block { display: flex; flex-direction: column; gap: 12px; }
.label { font-weight: 600; }
.actions { display: flex; flex-wrap: wrap; align-items: center; gap: 16px; padding-top: 6px; }
.toggles { display: flex; flex-direction: column; gap: 10px; }
.toggle { display: flex; align-items: center; gap: 10px; cursor: pointer; }
.toggle.off { color: var(--d-muted); cursor: default; }
.toggle input { width: 18px; height: 18px; margin: 0; accent-color: var(--d-go); }

.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 44px;
  padding: 0 20px;
  box-sizing: border-box;
  border: 1px solid var(--d-border);
  border-radius: 12px;
  background: var(--d-card);
  color: var(--d-ink);
  font: inherit;
  font-weight: 600;
  cursor: pointer;
}
.btn:hover:not(:disabled) { background: var(--d-hover); }
.btn:disabled { opacity: 0.55; cursor: default; }
.btn.go { min-height: 48px; padding: 0 26px; border-color: var(--d-go); background: var(--d-go); color: var(--d-on-go); font-size: 15px; }
.btn.go:hover:not(:disabled) { background: var(--d-go-hover); }
.btn.sm { min-height: 36px; padding: 0 14px; border-radius: 9px; font-size: 13px; }
.link { padding: 0; border: 0; background: none; color: var(--d-link); font: inherit; text-decoration: underline; cursor: pointer; }

.cmd { display: flex; align-items: center; gap: 8px; padding: 6px 6px 6px 16px; border-radius: 12px; background: var(--d-cmd); color: var(--d-cmd-fg); }
.cmd code { flex: 1; min-width: 0; font-family: var(--font-mono); font-size: 13px; overflow-wrap: anywhere; user-select: text; }
.cmd code.pre { white-space: pre-wrap; }
.copy { flex: none; min-height: 34px; padding: 0 12px; border: 0; border-radius: 8px; background: var(--d-copy); color: #ffffff; font: inherit; font-size: 12px; font-weight: 600; cursor: pointer; }
.copy:hover { background: var(--d-copy-hover); }

.pick { display: flex; flex-wrap: wrap; gap: 8px; }
.path-input { flex: 1 1 260px; min-height: 44px; padding: 0 14px; box-sizing: border-box; border: 1px solid var(--d-border); border-radius: 12px; background: var(--d-card); color: var(--d-ink); font-family: var(--font-mono); font-size: 12.5px; }
.path-input:disabled { color: var(--d-muted); }
.note, .field-error { margin: 0; }
.field-error { color: var(--d-error); user-select: text; }
.looked { color: var(--d-muted); }
.looked summary { cursor: pointer; }
.looked .path { padding-top: 6px; line-height: 1.8; }

.actions.first { padding-top: 0; }
code.inline { font-family: var(--font-mono); font-size: 12.5px; user-select: text; }
.failure { display: flex; flex-direction: column; gap: 8px; padding: 14px 16px; border: 1px solid var(--d-bad-bg); border-radius: 12px; background: var(--d-card); }
.failure strong { color: var(--d-bad-fg); }
.failure p { margin: 0; }
.out { margin: 0; padding: 10px 12px; max-height: 180px; overflow: auto; border-radius: 8px; background: var(--d-cmd); color: var(--d-cmd-fg); font-family: var(--font-mono); font-size: 12px; white-space: pre-wrap; user-select: text; }
.tips { margin: 0; padding-left: 18px; color: var(--d-muted); line-height: 1.5; }
.fix { padding: 12px 14px; border: 1px solid var(--d-line); border-radius: 12px; background: var(--d-card); }
.fix summary { font-weight: 600; cursor: pointer; }
.fix p { margin: 8px 0 10px; line-height: 1.45; color: var(--d-muted); }
.fix .cmd { margin-top: 6px; }

.moved { display: flex; align-items: flex-start; gap: 12px; padding: 14px 16px; border: 1px solid var(--d-warn-line); border-radius: 12px; background: var(--d-warn-bg); color: var(--d-warn-fg); line-height: 1.45; }
.moved > div { display: flex; flex-direction: column; gap: 4px; }
.bang { font-size: 16px; font-weight: 800; line-height: 1.2; }

.load-error { max-width: 520px; margin: 80px auto 0; }
.load-error p { color: var(--d-muted); user-select: text; }

.btn:focus-visible, .copy:focus-visible, .link:focus-visible, .path-input:focus-visible, .toggle input:focus-visible, summary:focus-visible {
  outline: 3px solid #7b5cff;
  outline-offset: 2px;
}
@media (prefers-reduced-motion: reduce) {
  .mark { animation: none; }
}
</style>
