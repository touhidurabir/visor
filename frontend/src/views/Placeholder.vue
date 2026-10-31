<script setup lang="ts">
import { useRoute } from 'vue-router'

const route = useRoute()
</script>

<template>
  <section>
    <h1>{{ route.meta.title }}</h1>
    <p class="muted">This screen arrives in a later task of Visor v0.1.0.</p>
  </section>
</template>

<style scoped>
h1 { margin: 0 0 8px; font-size: 22px; font-weight: 650; }
.muted { color: var(--text-muted); }
</style>
