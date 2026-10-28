<template>
  <div class="page">
    <div class="page-title"><h2>缺料預警</h2></div>
    <div class="card">
      <el-tabs v-model="tab">
        <el-tab-pane label="目前缺料（即時）" name="now">
          <ShortageList ref="shortages" @changed="loadAlerts" />
        </el-tab-pane>
        <el-tab-pane label="預警紀錄" name="log">
          <div class="bar">
            <p class="muted rule">
              開立製令時檢查：可用量（庫存 ＋ 在途製令與採購 − 其他製令未扣料）低於需求 × (1＋緩衝%) 即預警。
              材料會自動加入採購單草稿；半成品由生管選「通知建單」或 Pass。
            </p>
            <el-radio-group v-model="status" size="small" @change="loadAlerts">
              <el-radio-button value="PENDING">待處理</el-radio-button>
              <el-radio-button value="">全部</el-radio-button>
            </el-radio-group>
          </div>
          <AlertList :alerts="alerts" @decided="loadAlerts" />
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import AlertList, { type Alert } from '../components/AlertList.vue'
import ShortageList from '../components/purchasing/ShortageList.vue'

const tab = ref('now')
const status = ref('')
const alerts = ref<Alert[]>([])
const shortages = ref<InstanceType<typeof ShortageList>>()

async function loadAlerts() {
  alerts.value = await api<Alert[]>(`/alerts?status=${status.value}`)
  shortages.value?.load()
}
onMounted(loadAlerts)
</script>

<style scoped>
.bar { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; margin-bottom: 10px; flex-wrap: wrap; }
.rule { margin: 0; font-size: 12px; max-width: 760px; }
</style>
