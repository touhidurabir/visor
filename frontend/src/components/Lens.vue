<script setup lang="ts">
// The visor lens (spec §5.1): an iridescent disc with a ring of one segment
// per check. On mount the segments light in order and one sweep crosses the
// glass; reduced motion turns both off. Re-key it to play them again.
defineProps<{
  segments: string[] // 'ok' | 'bad' | 'wait', one per check
  big: string
  small: string
}>()
</script>

<template>
  <div class="lens-wrap">
    <svg viewBox="0 0 340 340" class="ring" aria-hidden="true">
      <g transform="rotate(-90 170 170)">
        <circle v-for="(s, i) in segments" :key="i" cx="170" cy="170" r="160" :class="['arc', `arc-${i}`, `c-${s}`]" />
      </g>
    </svg>
    <div class="lens">
      <div class="glass">
        <span class="big">{{ big }}</span>
        <span class="small">{{ small }}</span>
      </div>
      <div class="sweep" aria-hidden="true" />
    </div>
  </div>
</template>

<style scoped>
.lens-wrap { position: relative; width: var(--lens-size, 340px); max-width: 100%; aspect-ratio: 1 / 1; }
.ring { position: absolute; inset: 0; width: 100%; height: 100%; }
.arc { fill: none; stroke-width: 9; stroke-linecap: round; stroke-dasharray: 187 818; animation: light 0.45s ease-out both; }
.arc-0 { stroke-dashoffset: -7px; animation-delay: 0.15s; }
.arc-1 { stroke-dashoffset: -208px; animation-delay: 0.5s; }
.arc-2 { stroke-dashoffset: -409px; animation-delay: 0.85s; }
.arc-3 { stroke-dashoffset: -610px; animation-delay: 1.2s; }
.arc-4 { stroke-dashoffset: -811px; animation-delay: 1.55s; }
.c-ok { stroke: var(--lens-ok, #1e9e63); }
.c-bad { stroke: var(--lens-bad, #d92d20); }
.c-wait { stroke: var(--lens-wait, #c9d3df); }
.lens {
  position: absolute;
  inset: 9%;
  border-radius: 50%;
  background: conic-gradient(from 200deg, #19b5c8, #5b7cff, #9b5cff, #e86bb8, #ffb547, #7ee0c3, #19b5c8);
  box-shadow: 0 30px 60px -20px rgba(43, 58, 120, 0.45), inset 0 0 0 1px rgba(255, 255, 255, 0.5);
}
.glass {
  position: absolute;
  inset: 4.5%;
  border-radius: 50%;
  background:
    radial-gradient(circle at 34% 26%, rgba(255, 255, 255, 0.38) 0, rgba(255, 255, 255, 0) 30%),
    radial-gradient(circle at 50% 120%, rgba(123, 92, 255, 0.55) 0, rgba(123, 92, 255, 0) 55%),
    #14202e;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  color: #ffffff;
  text-align: center;
}
.big {
  font-family: 'Bricolage Grotesque Variable', -apple-system, system-ui, sans-serif;
  font-optical-sizing: auto;
  font-size: calc(var(--lens-size, 340px) * 0.19);
  font-weight: 750;
  line-height: 1;
}
.small { max-width: 60%; font-size: 14px; color: #c9d3e3; }
.sweep {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: conic-gradient(from 0deg, rgba(255, 255, 255, 0) 0deg, rgba(255, 255, 255, 0) 290deg, rgba(255, 255, 255, 0.75) 360deg);
  mix-blend-mode: soft-light;
  animation: spin 1.75s linear 0.1s 1 both, fade 0.5s ease 1.85s forwards;
  pointer-events: none;
}
@keyframes spin { from { transform: rotate(0); } to { transform: rotate(360deg); } }
@keyframes fade { to { opacity: 0; } }
@keyframes light { from { stroke: var(--lens-wait, #c9d3df); opacity: 0.35; } }
@media (prefers-reduced-motion: reduce) {
  .arc, .sweep { animation: none; }
  .sweep { opacity: 0; }
}
</style>
