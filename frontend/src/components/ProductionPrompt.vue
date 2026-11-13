<script setup lang="ts">
// The question a production server asks before an action that interrupts
// processes (lib/production.ts), naming the server.
import { productionAsk } from '../lib/production'
import ConfirmDialog from './ConfirmDialog.vue'
</script>

<template>
  <ConfirmDialog
    v-if="productionAsk"
    :title="productionAsk.title"
    :message="`${productionAsk.message} ${productionAsk.server} is tagged Production, so Visor asks first.`"
    :confirm-label="productionAsk.label"
    @confirm="productionAsk.answer(true)"
    @cancel="productionAsk.answer(false)"
  />
</template>
