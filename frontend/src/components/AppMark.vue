<script setup lang="ts">
// Visor's mark: the app icon (option C, "Pulse") without its tile, i.e. the
// iridescent ring, the dark glass and the mint pulse line. Same geometry as
// scripts/appicon (1024 grid, ring r296, glass r262), cropped to the ring.
// The colours are the icon's own, not theme tokens, so the mark matches the
// Dock icon in light and dark. Size it with --mark-size (default 22px).
import { useId } from 'vue'

const id = useId()
const iri = `iri-${id}`
const glass = `glass-${id}`
</script>

<template>
  <svg class="app-mark" viewBox="216 216 592 592" aria-hidden="true" focusable="false">
    <defs>
      <linearGradient :id="iri" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stop-color="#19B5C8" />
        <stop offset=".28" stop-color="#5B7CFF" />
        <stop offset=".5" stop-color="#9B5CFF" />
        <stop offset=".72" stop-color="#E86BB8" />
        <stop offset="1" stop-color="#FFB547" />
      </linearGradient>
      <radialGradient :id="glass" cx=".38" cy=".3" r=".9">
        <stop offset="0" stop-color="#4A5568" />
        <stop offset=".45" stop-color="#16202E" />
        <stop offset="1" stop-color="#2E2A6B" />
      </radialGradient>
    </defs>
    <circle cx="512" cy="512" r="296" :fill="`url(#${iri})`" />
    <circle cx="512" cy="512" r="262" :fill="`url(#${glass})`" />
    <path
      d="M290 520 H420 L462 430 L522 620 L566 490 L598 520 H734"
      fill="none"
      stroke="#7EE0C3"
      stroke-width="34"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
  </svg>
</template>

<style scoped>
.app-mark { display: block; width: var(--mark-size, 22px); height: var(--mark-size, 22px); flex: none; }
</style>
