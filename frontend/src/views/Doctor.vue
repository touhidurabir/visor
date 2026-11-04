<script setup lang="ts">
// Doctor (spec §5.1, board 1 · First launch / setup): the five setup checks
// beside the lens, each with its status in words, and under them one card
// with the fix for the first one that fails (what's wrong → what to do →
// Check again). On first launch
// (firstRun) it fills the window until setup is finished; it is also the
// Doctor page in the sidebar. Visor never runs sudo or installs anything:
// every fix is a command to copy or a file to choose.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { DoctorService, SettingsService, type DoctorView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import Lens from '../components/Lens.vue'
import { copyText } from '../lib/clipboard'
import { chownCommand, installCommands, lensText, mkdirCommand, rootFixes, situationCopy, startCommand, startTips, stepLook } from '../lib/doctor'
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
    <!-- First launch fills the window: a title bar strip under the traffic
         lights (hidden-inset title bar), so it reads as the app, not a page. -->
    <div v-if="firstRun" class="titlebar"><span>Visor</span></div>

    <div class="body">
      <div v-if="loadError && !view" class="load-error" role="alert">
        <strong>Couldn’t run the checks</strong>
        <p>{{ loadError }}</p>
        <button type="button" class="btn" @click="run">Try again</button>
      </div>

      <div v-else-if="view" class="layout">
        <div class="lens-col">
          <Lens :key="runs" :segments="segments" :big="lens.big" :small="lens.small" :pulse="lens.big === '✓'" />
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

          <ol class="card checks" aria-label="What Visor checked">
            <li v-for="(st, i) in steps" :key="st.ID" class="step" :data-status="st.Status">
              <span :class="['mark', `m-${stepLook(st.Status).tone}`, `d-${i}`]" aria-hidden="true">{{ stepLook(st.Status).glyph }}</span>
              <span class="step-text">
                <span :class="['step-title', { 't-wait': st.Status === 'wait' }]">{{ st.Title }}</span>
                <span v-if="st.Detail" class="path">{{ st.Detail }}</span>
              </span>
              <span :class="['badge', 'word', `b-${stepLook(st.Status).tone}`]" data-testid="step-word">{{ stepLook(st.Status).word }}</span>
            </li>
          </ol>

          <!-- What to do: one card under the checks, for every situation. -->
          <section class="card fix" aria-label="What to do">
            <!-- All found -->
            <template v-if="situation === 'ready'">
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
                <button type="button" class="btn primary lg" :disabled="finishing" @click="openDashboard(true)">Open dashboard</button>
                <span class="meta">Visor also lives in your menu bar.</span>
              </div>
            </template>

            <!-- Not installed -->
            <template v-if="situation === 'nosup'">
              <h2 class="sec">Run these in Terminal, then check again</h2>
              <div v-for="c in installCommands" :key="c" class="cmd">
                <code>{{ c }}</code>
                <button type="button" class="btn copy" @click="copy(c)">{{ copied === c ? 'Copied' : 'Copy' }}</button>
              </div>
              <span class="meta">Visor never installs anything or asks for your password.</span>
              <div class="actions">
                <button type="button" class="btn primary lg" :disabled="checking" @click="run">{{ checking ? 'Checking…' : 'Check again' }}</button>
                <button v-if="!chooserOpen" type="button" class="btn" @click="chooserOpen = true">It’s installed somewhere else…</button>
              </div>
            </template>

            <!-- No config / installed somewhere else -->
            <div v-if="showChooser" :class="['part', { split: situation === 'nosup' }]">
              <label for="doctor-conf" class="sec">Your supervisord.conf</label>
              <div class="pick">
                <input
                  id="doctor-conf"
                  v-model.trim="pathDraft"
                  class="input path-input"
                  type="text"
                  spellcheck="false"
                  autocomplete="off"
                  placeholder="/path/to/supervisord.conf"
                  :disabled="view.Overridden"
                  @keydown.enter="useConfig"
                />
                <button type="button" class="btn" :disabled="view.Overridden" @click="choose">Choose…</button>
              </div>
              <p v-if="view.Overridden" class="meta note">Set by VISOR_CONFIG. Unset it to choose a file here.</p>
              <p v-if="pathError" class="field-error" role="alert">{{ pathError }}</p>
              <details v-if="view.Looked?.length" class="looked">
                <summary>Where Visor looked</summary>
                <div class="path">
                  <div v-for="p in view.Looked" :key="p">{{ p }}</div>
                </div>
              </details>
              <div class="actions">
                <button type="button" class="btn primary lg" :disabled="saving || view.Overridden || !pathDraft" @click="useConfig">
                  {{ saving ? 'Checking…' : 'Use this config' }}
                </button>
              </div>
            </div>

            <!-- Not running -->
            <template v-if="situation === 'down'">
              <h2 class="sec">Start it as your user</h2>
              <template v-if="view.CanStart">
                <span class="meta">Visor runs <code class="inline">{{ view.StartCommand }}</code> as you. It never uses sudo.</span>
                <div class="actions">
                  <button type="button" class="btn primary lg" :disabled="starting || checking" @click="startSupervisord">
                    {{ starting ? 'Starting…' : 'Start supervisord' }}
                  </button>
                  <button type="button" class="btn" :disabled="starting || checking" @click="run">{{ checking ? 'Checking…' : 'Check again' }}</button>
                  <button type="button" class="link" :aria-expanded="rootHelpOpen" @click="rootHelpOpen = !rootHelpOpen">supervisord runs as root?</button>
                </div>
              </template>
              <template v-else>
                <p v-if="view.Foreground" class="meta note">
                  This config keeps supervisord in the foreground (<code class="inline">nodaemon=true</code>), the way launchd runs it, so Visor
                  doesn’t start it. Start it the way you set it up, or run this in Terminal:
                </p>
                <div class="cmd">
                  <code>{{ manualStart }}</code>
                  <button type="button" class="btn copy" @click="copy(manualStart)">{{ copied === manualStart ? 'Copied' : 'Copy' }}</button>
                </div>
                <div class="actions">
                  <button type="button" class="btn primary lg" :disabled="checking" @click="run">{{ checking ? 'Checking…' : 'Check again' }}</button>
                  <button type="button" class="link" :aria-expanded="rootHelpOpen" @click="rootHelpOpen = !rootHelpOpen">supervisord runs as root?</button>
                </div>
              </template>

              <div v-if="view.StartFailure" class="failure" role="alert">
                <strong>supervisord didn’t start</strong>
                <p class="meta">Visor ran <code class="inline">{{ view.StartFailure.Command }}</code>.</p>
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
            </template>

            <!-- Root-only socket (and "supervisord runs as root?") -->
            <div v-if="situation === 'denied' || (situation === 'down' && rootHelpOpen)" :class="['part', { split: situation === 'down' }]">
              <component :is="situation === 'denied' ? 'h2' : 'h3'" class="sec">Pick one, then check again</component>
              <details v-for="(f, i) in fixes" :key="f.title" class="option" :open="i === 0">
                <summary>{{ f.title }}</summary>
                <p>{{ f.why }}</p>
                <div v-for="c in f.commands" :key="c" class="cmd">
                  <code class="pre">{{ c }}</code>
                  <button type="button" class="btn copy" @click="copy(c)">{{ copied === c ? 'Copied' : 'Copy' }}</button>
                </div>
              </details>
              <span class="meta">Visor never runs sudo or changes these files for you.</span>
              <div v-if="situation === 'denied'" class="actions">
                <button type="button" class="btn primary lg" :disabled="checking" @click="run">{{ checking ? 'Checking…' : 'Check again' }}</button>
              </div>
            </div>

            <!-- Wrong username or password -->
            <div v-if="situation === 'auth'" class="actions">
              <button type="button" class="btn primary lg" @click="openSettings">Open Settings</button>
              <button type="button" class="btn" :disabled="checking" @click="run">{{ checking ? 'Checking…' : 'Check again' }}</button>
            </div>

            <!-- Can't edit configs -->
            <template v-if="situation === 'notwritable'">
              <template v-if="folderFix.command">
                <h2 class="sec">{{ folderFix.label }}</h2>
                <div class="cmd">
                  <code>{{ folderFix.command }}</code>
                  <button type="button" class="btn copy" @click="copy(folderFix.command)">{{ copied === folderFix.command ? 'Copied' : 'Copy' }}</button>
                </div>
              </template>
              <span class="meta">Everything else works: watching, starting, stopping and logs.</span>
              <div class="actions">
                <button type="button" class="btn primary lg" :disabled="finishing" @click="openDashboard(false)">Open dashboard anyway</button>
                <button type="button" class="btn" :disabled="checking" @click="run">{{ checking ? 'Checking…' : 'Check again' }}</button>
              </div>
            </template>
          </section>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Board 1 (DoctorNew), on the shared tokens and .btn / .badge / .input. */
.doctor {
  /* The lens ring in the app's state colours. */
  --lens-ok: var(--ok-dot);
  --lens-bad: var(--bad-dot);
  --lens-wait: var(--line2);
  min-height: 100vh;
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  background: var(--side);
  color: var(--ink);
}
.titlebar {
  flex: none;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-bottom: 1px solid var(--line);
  background: var(--side);
  color: var(--ink3);
  font-size: 12.5px;
  font-weight: 600;
}
.body { flex: 1; display: flex; align-items: center; justify-content: center; padding: 40px 48px; }
/* As a page beside the sidebar: the content area's own surface and padding. */
.doctor.embedded { min-height: 100%; background: transparent; --lens-size: 260px; }
.embedded .body { padding: 0; }
.layout {
  width: 100%;
  max-width: 1120px;
  margin: 0 auto;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 56px;
}
.embedded .layout { gap: 40px; }
.lens-col { flex: 0 1 var(--lens-size, 320px); display: flex; justify-content: center; --lens-size: 320px; }
.embedded .lens-col { --lens-size: 260px; }
.text-col { flex: 1 1 420px; max-width: 540px; display: flex; flex-direction: column; gap: 20px; }
.intro { display: flex; flex-direction: column; gap: 8px; }
.display { margin: 0; font-family: var(--display); font-optical-sizing: auto; font-size: 40px; font-weight: 650; line-height: 1.05; letter-spacing: -0.015em; }
.embedded .display { font-size: 34px; }
.lede { margin: 0; max-width: 46ch; font-size: 15px; line-height: 1.5; color: var(--ink2); }
.meta { color: var(--ink3); font-size: 12.5px; }
.sec { margin: 0; font-size: 15px; font-weight: 600; }
.path { font-family: var(--mono); font-size: 12px; color: var(--ink3); overflow-wrap: anywhere; user-select: text; }
.card { border: 1px solid var(--line); border-radius: 10px; background: var(--raised); }

/* The checks: mark, title and detail, status word. */
.checks { list-style: none; margin: 0; padding: 4px 0; }
.step { display: grid; grid-template-columns: 26px minmax(0, 1fr) auto; column-gap: 12px; align-items: start; padding: 10px 16px; border-top: 1px solid var(--line); }
.step:first-child { border-top: 0; }
.step-text { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
.step-title { font-weight: 600; }
.t-wait { color: var(--ink2); }
.word { height: 20px; font-size: 11.5px; }
.mark { width: 24px; height: 24px; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 13px; font-weight: 700; animation: pop 0.35s ease-out both; }
.m-ok { background: var(--ok-soft); color: var(--ok); }
.m-bad { background: var(--bad-soft); color: var(--bad); }
.m-idle { background: var(--idle-soft); color: var(--idle); }
.d-0 { animation-delay: 0.15s; }
.d-1 { animation-delay: 0.5s; }
.d-2 { animation-delay: 0.85s; }
.d-3 { animation-delay: 1.2s; }
.d-4 { animation-delay: 1.55s; }
@keyframes pop { 0% { transform: scale(0.6); opacity: 0; } 70% { transform: scale(1.08); } 100% { transform: scale(1); opacity: 1; } }

/* What to do. */
.fix { display: flex; flex-direction: column; gap: 12px; padding: 16px 18px; }
.part { display: flex; flex-direction: column; gap: 12px; }
.part.split { padding-top: 14px; border-top: 1px solid var(--line); }
.label { font-weight: 600; }
.actions { display: flex; flex-wrap: wrap; align-items: center; gap: 10px 14px; padding-top: 4px; }
.toggles { display: flex; flex-direction: column; gap: 10px; }
.toggle { display: flex; align-items: center; gap: 10px; cursor: pointer; }
.toggle.off { color: var(--ink3); cursor: default; }
.toggle input { width: 16px; height: 16px; margin: 0; accent-color: var(--accent); }
.link { padding: 0; border: 0; background: none; color: var(--accent-text); font: inherit; text-decoration: underline; text-underline-offset: 2px; cursor: pointer; }

/* Command blocks stay dark, like a terminal, with a 30 px Copy. */
.cmd { display: flex; align-items: center; gap: 8px; padding: 5px 5px 5px 14px; border-radius: 8px; background: var(--log-bg); color: var(--log-ink); }
.cmd code { flex: 1; min-width: 0; font-family: var(--mono); font-size: 13px; overflow-wrap: anywhere; user-select: text; }
.cmd code.pre { white-space: pre-wrap; }
.btn.copy { flex: none; border-color: transparent; background: color-mix(in srgb, var(--log-ink) 12%, transparent); color: var(--log-ink); }
.btn.copy:hover:not(:disabled) { background: color-mix(in srgb, var(--log-ink) 20%, transparent); }

.pick { display: flex; flex-wrap: wrap; gap: 8px; }
.path-input { flex: 1 1 260px; font-family: var(--mono); font-size: 12.5px; }
.path-input:disabled { background: var(--sunken); color: var(--ink3); }
.note, .field-error { margin: 0; }
.field-error { color: var(--bad); user-select: text; }
.looked { color: var(--ink2); }
.looked summary { cursor: pointer; }
.looked .path { padding-top: 6px; line-height: 1.8; }

code.inline { font-family: var(--mono); font-size: 12.5px; user-select: text; }
.failure { display: flex; flex-direction: column; gap: 8px; padding: 12px 14px; border: 1px solid var(--error-card-border); border-radius: 8px; background: var(--bad-soft); }
.failure strong { color: var(--bad); }
.failure p { margin: 0; }
.out { margin: 0; padding: 10px 12px; max-height: 180px; overflow: auto; border-radius: 8px; background: var(--log-bg); color: var(--log-ink); font-family: var(--mono); font-size: 12px; white-space: pre-wrap; user-select: text; }
.tips { margin: 0; padding-left: 18px; color: var(--ink2); line-height: 1.5; }
.option { padding: 10px 12px; border: 1px solid var(--line); border-radius: 8px; background: var(--sunken); }
.option summary { font-weight: 600; cursor: pointer; }
.option p { margin: 8px 0 10px; line-height: 1.45; color: var(--ink2); }
.option .cmd { margin-top: 6px; }

.moved { display: flex; align-items: flex-start; gap: 12px; padding: 12px 14px; border: 1px solid var(--warn-dot); border-radius: 10px; background: var(--warn-soft); color: var(--warn); line-height: 1.45; }
.moved > div { display: flex; flex-direction: column; gap: 4px; }
.bang { font-size: 16px; font-weight: 800; line-height: 1.2; }

.load-error { max-width: 520px; }
.load-error p { color: var(--ink2); user-select: text; }

@media (prefers-reduced-motion: reduce) {
  .mark { animation: none; }
}
</style>
