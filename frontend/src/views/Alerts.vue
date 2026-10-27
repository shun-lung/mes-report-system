<template>
  <div class="page">
    <div class="page-title">
      <h2>缺料預警</h2>
      <el-radio-group v-model="status" size="small" @change="load">
        <el-radio-button value="PENDING">待處理</el-radio-button>
        <el-radio-button value="">全部</el-radio-button>
      </el-radio-group>
    </div>
    <div class="card">
      <p class="muted rule">
        規則：可用量（庫存 ＋ 在途半成品製令 − 其他製令未扣料需求）低於本製令未扣料需求 × 103% 時預警。
        「通知建單」會依當下缺口建立草稿半成品製令。
      </p>
      <AlertList :alerts="alerts" @decided="load" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import AlertList, { type Alert } from '../components/AlertList.vue'

const status = ref('PENDING')
const alerts = ref<Alert[]>([])

async function load() {
  alerts.value = await api<Alert[]>(`/alerts?status=${status.value}`)
}
onMounted(load)
</script>

<style scoped>
.rule { margin: 0 0 12px; font-size: 12px; }
</style>
