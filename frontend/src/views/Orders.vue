<template>
  <div class="page">
    <div class="page-title"><h2>客戶訂單與製令</h2></div>

    <div class="card">
      <h3>客戶訂單 <small>製造需求來源</small></h3>
      <el-table :data="orders" size="small">
        <el-table-column label="訂單" min-width="190">
          <template #default="{ row }"><b>{{ row.soNo }}</b><div class="muted small">客戶 PO：{{ row.customerPo ?? '—' }}</div></template>
        </el-table-column>
        <el-table-column label="客戶" prop="customer" min-width="160" />
        <el-table-column label="訂單日" width="100">
          <template #default="{ row }"><span class="num">{{ fmt.date(row.orderDate) }}</span></template>
        </el-table-column>
        <el-table-column label="狀態" width="90">
          <template #default="{ row }"><el-tag size="small">{{ soStatus[row.status] }}</el-tag></template>
        </el-table-column>
        <el-table-column label="明細 → 製令" min-width="460">
          <template #default="{ row }">
            <div v-for="l in row.lines" :key="l.lineNo" class="so-line">
              <span>{{ l.lineNo }}. {{ l.itemNo }} {{ l.itemName }}</span>
              <span class="num">{{ fmt.int(l.qty) }} 雙</span>
              <span class="muted num">交期 {{ fmt.date(l.dueDate) }}</span>
              <span v-if="l.woNo">→ {{ l.woNo }} <span class="muted">（{{ statusText[l.woStatus] }}，完工 {{ fmt.int(l.finishedQty) }}）</span></span>
              <span v-else class="muted">未轉製令</span>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="card">
      <h3>製令單 <small>{{ workOrders.length }} 張</small></h3>
      <el-table :data="workOrders" size="small">
        <el-table-column label="製令" min-width="190">
          <template #default="{ row }">
            <b>{{ row.woNo }}</b>
            <div class="small">
              <el-tag size="small">{{ statusText[row.status] }}</el-tag>
              <el-tag v-if="row.source === 'SHORTAGE'" size="small" type="warning" class="ml">缺料衍生</el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="品項" min-width="170">
          <template #default="{ row }">{{ row.itemNo }}<div class="muted small">{{ row.itemName }}</div></template>
        </el-table-column>
        <el-table-column label="來源" min-width="170">
          <template #default="{ row }">
            <template v-if="row.soNo">{{ row.soNo }}<div class="muted small">{{ row.customer }}</div></template>
            <template v-else-if="row.parentWoNo">缺料支援 {{ row.parentWoNo }}</template>
          </template>
        </el-table-column>
        <el-table-column label="流程卡" prop="cards" width="80" align="right" />
        <el-table-column label="各站（投入 / 良品 / 不良）" min-width="300">
          <template #default="{ row }">
            <div v-for="s in row.steps" :key="s.seq" class="num small">
              {{ s.seq }} {{ s.station }}：{{ fmt.int(s.input) }} / {{ fmt.int(s.good) }} / {{ fmt.int(s.defect) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="完工 / 計畫" width="120" align="right">
          <template #default="{ row }"><span class="num">{{ fmt.int(row.finishedQty) }} / {{ fmt.int(row.plannedQty) }}</span></template>
        </el-table-column>
        <el-table-column label="操作" width="210">
          <template #default="{ row }">
            <el-button v-if="row.status === 'DRAFT' && session.can('wo:release')" size="small" type="primary" @click="release(row.id)">開立</el-button>
            <el-button v-if="['RELEASED', 'IN_PROGRESS'].includes(row.status) && session.can('inventory:edit')" size="small" @click="issue(row.id)">發料</el-button>
            <el-button v-if="row.cards" size="small" @click="printCards(row.id)">列印流程卡</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, fmt } from '../api'
import { useSession } from '../stores/session'
import { statusText, type WorkOrder } from '../components/dashboard/types'

const session = useSession()
const orders = ref<any[]>([])
const workOrders = ref<WorkOrder[]>([])
const soStatus: Record<string, string> = { OPEN: '未生產', IN_PRODUCTION: '生產中', COMPLETED: '已完成', CLOSED: '結案', CANCELLED: '取消' }

async function load() {
  ;[orders.value, workOrders.value] = await Promise.all([api<any[]>('/sales-orders'), api<WorkOrder[]>('/work-orders')])
}
async function release(id: string) {
  try {
    const r = await api<{ woNo: string; cards: number; alerts: string[] }>(`/work-orders/${id}/release`, { method: 'POST' })
    ElMessage.success(`${r.woNo} 已開立，流程卡 ${r.cards} 張${r.alerts.length ? `，缺料預警 ${r.alerts.length} 筆` : ''}；可按「列印流程卡」`)
    load()
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}
function printCards(id: string) {
  window.open(`/print/cards/${id}`, '_blank')
}
async function issue(id: string) {
  try {
    const r = await api<{ issuedLines: number }>(`/work-orders/${id}/issue`, { method: 'POST' })
    ElMessage.success(r.issuedLines ? `已發料 ${r.issuedLines} 項` : '沒有待發料項目')
    load()
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}
onMounted(load)
</script>

<style scoped>
.small { font-size: 12px; }
.ml { margin-left: 4px; }
.so-line { display: flex; flex-wrap: wrap; gap: 4px 12px; line-height: 1.8; }
</style>
