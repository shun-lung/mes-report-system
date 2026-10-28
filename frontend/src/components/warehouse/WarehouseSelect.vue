<template>
  <el-select v-model="value" placeholder="選擇倉庫" class="wh">
    <el-option v-for="w in warehouses.filter((x) => x.isActive)" :key="w.id" :label="`${w.code} ${w.name}`" :value="w.id" />
  </el-select>
</template>

<script setup lang="ts">
export interface WH { id: string; code: string; name: string; type: string; stationId: string | null; stationName: string | null; isActive: boolean; items: number }
defineProps<{ warehouses: WH[] }>()
const value = defineModel<string>({ required: true })
</script>

<style scoped>
.wh { width: 200px; }
</style>
