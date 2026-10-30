<script setup lang="ts">
import ProcessList from './views/ProcessList.vue'
</script>

<template>
  <main class="shell">
    <h1>Visor</h1>
    <ProcessList />
  </main>
</template>

<style scoped>
.shell {
  padding: 56px 24px 24px;
}
h1 {
  margin: 0 0 12px;
  font-size: 20px;
  font-weight: 600;
}
</style>
