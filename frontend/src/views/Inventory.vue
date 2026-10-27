<template>
  <div class="page">
    <div class="page-title"><h2>庫存與異動歷程</h2></div>
    <div class="card">
      <h3>庫存餘額</h3>
      <el-table :data="balances" size="small" @row-click="(r: any) => filter(r.productId, r.itemNo)">
        <el-table-column label="倉庫" prop="warehouseName" width="120" />
        <el-table-column label="品號" prop="itemNo" width="100" />
        <el-table-column label="品名" prop="itemName" min-width="160" />
        <el-table-column label="類別" width="90">
          <template #default="{ row }">{{ catName[row.category] }}</template>
        </el-table-column>
        <el-table-column label="數量" align="right" width="130">
          <template #default="{ row }">
            <span class="num" :class="{ neg: row.qty < 0 }">{{ qty(row.qty) }}</span> {{ row.unit }}
          </template>
        </el-table-column>
        <el-table-column label="最後異動" width="130">
          <template #default="{ row }"><span class="num">{{ fmt.dateTime(row.updatedAt, tz) }}</span></template>
        </el-table-column>
      </el-table>
      <p class="muted small">點選列可篩選下方該料件的歷程</p>
    </div>

    <div class="card">
      <h3>
        異動歷程 <small>只增不改，錯誤以沖銷處理</small>
        <el-tag v-if="productLabel" closable size="small" @close="filter('', '')">{{ productLabel }}</el-tag>
      </h3>
      <el-radio-group v-model="type" size="small" class="types" @change="loadTxns">
        <el-radio-button value="">全部</el-radio-button>
        <el-radio-button v-for="(v, key) in txnName" :key="key" :value="key">{{ v }}</el-radio-button>
      </el-radio-group>
      <el-table :data="txns" size="small" max-height="560">
        <el-table-column label="時間" width="110">
          <template #default="{ row }"><span class="num">{{ fmt.dateTime(row.occurredAt, tz) }}</span></template>
        </el-table-column>
        <el-table-column label="單號" prop="txnNo" width="190" />
        <el-table-column label="類型" width="100">
          <template #default="{ row }">{{ txnName[row.txnType] ?? row.txnType }}</template>
        </el-table-column>
        <el-table-column label="倉庫" prop="warehouseName" width="110" />
        <el-table-column label="料件" min-width="170">
          <template #default="{ row }">{{ row.itemNo }} <span class="muted">{{ row.itemName }}</span></template>
        </el-table-column>
        <el-table-column label="數量" align="right" width="100">
          <template #default="{ row }"><span class="num" :class="row.qty < 0 ? 'out' : 'in'">{{ row.qty > 0 ? '+' : '' }}{{ qty(row.qty) }}</span></template>
        </el-table-column>
        <el-table-column label="結存" align="right" width="100">
          <template #default="{ row }"><span class="num">{{ qty(row.balanceAfter) }}</span></template>
        </el-table-column>
        <el-table-column label="製令" prop="woNo" width="190" />
        <el-table-column label="人員" prop="userName" width="80" />
        <el-table-column label="說明" prop="note" min-width="160" />
      </el-table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, fmt } from '../api'
import { useSession } from '../stores/session'

const session = useSession()
const tz = computed(() => session.factory?.timezone ?? 'Asia/Taipei')
const balances = ref<any[]>([])
const txns = ref<any[]>([])
const type = ref('')
const productId = ref('')
const productLabel = ref('')
const catName: Record<string, string> = { FG: '成品', SFG: '半成品', RAW: '基本材料', AUX: '輔助料件' }
const txnName: Record<string, string> = {
  RECEIPT: '收料入庫', ISSUE: '發料出庫', CONSUME: '到站扣帳', STATION_IN: '暫存倉入庫', STATION_OUT: '暫存倉出庫',
  FG_IN: '成品入庫', ADJUST: '調整', REVERSAL: '沖銷',
}
const qty = (v: number) => fmt.dec(v, v % 1 === 0 ? 0 : 2)

async function loadTxns() {
  const q = new URLSearchParams({ limit: '500' })
  if (type.value) q.set('type', type.value)
  if (productId.value) q.set('productId', productId.value)
  txns.value = await api<any[]>('/inventory/transactions?' + q)
}
function filter(id: string, label: string) {
  productId.value = id
  productLabel.value = label
  loadTxns()
}
onMounted(async () => {
  balances.value = await api<any[]>('/inventory/balances')
  loadTxns()
})
</script>

<style scoped>
.small { font-size: 12px; margin: 8px 0 0; }
.types { margin-bottom: 10px; flex-wrap: wrap; }
.neg, .out { color: var(--div-neg-ink); }
.in { color: var(--div-pos-ink); }
</style>
