<template>
  <div>
    <div class="bar">
      <span class="muted small">彙總所有已開立製令尚未扣料的需求，對照庫存與在途（半成品製令、採購單含草稿），即時計算。</span>
      <span class="spacer" />
      <el-radio-group v-model="filter" size="small">
        <el-radio-button value="short">缺料（{{ counts.SHORT }}）</el-radio-button>
        <el-radio-button value="all">全部（{{ list.length }}）</el-radio-button>
      </el-radio-group>
      <el-button v-if="canPurchase" type="primary" size="small" :disabled="!selected.length" :loading="busy" @click="purchase">
        產生採購單（{{ selected.length }}）
      </el-button>
    </div>
    <el-table :data="shown" size="small" empty-text="目前沒有缺料" @selection-change="(rows: Shortage[]) => (selected = rows)">
      <el-table-column v-if="canPurchase" type="selection" width="36" :selectable="(r: Shortage) => r.purchasable && r.status === 'SHORT'" />
      <el-table-column type="expand" width="32">
        <template #default="{ row }">
          <div class="wos">
            <div class="muted small">需要此料件的製令：</div>
            <div v-for="w in row.workOrders" :key="w.woNo" class="wo">
              {{ w.woNo }} <span class="muted">（{{ statusText[w.status] }}，交期 {{ fmt.date(w.dueDate) }}）</span>
              未扣 <b class="num">{{ q(w.qty) }}</b> {{ row.unit }}
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="料件" min-width="170">
        <template #default="{ row }">
          <b>{{ row.itemNo }}</b> {{ row.itemName }}
          <div class="muted small">{{ categoryName[row.category] }}<template v-if="row.supplier"> · {{ row.supplier }}</template></div>
        </template>
      </el-table-column>
      <el-table-column label="未扣需求" width="96" align="right"><template #default="{ row }"><span class="num">{{ q(row.need) }}</span></template></el-table-column>
      <el-table-column label="庫存" width="90" align="right"><template #default="{ row }"><span class="num">{{ q(row.onHand) }}</span></template></el-table-column>
      <el-table-column label="在途" width="120" align="right">
        <template #default="{ row }">
          <span class="num">{{ q(row.inboundWo + row.inboundPo) }}</span>
          <div class="muted small"><template v-if="row.inboundWo">製令 {{ q(row.inboundWo) }} </template><template v-if="row.inboundPo">採購 {{ q(row.inboundPo) }}</template></div>
        </template>
      </el-table-column>
      <el-table-column label="淨缺口" width="96" align="right">
        <template #default="{ row }"><b class="num" :class="{ bad: row.shortage > 0 }">{{ row.shortage > 0 ? q(row.shortage) : '—' }}</b></template>
      </el-table-column>
      <el-table-column label="狀態" width="120">
        <template #default="{ row }"><span class="status" :class="row.status"><i />{{ statusName[row.status] }}</span></template>
      </el-table-column>
      <el-table-column label="處理" width="130">
        <template #default="{ row }">
          <template v-if="row.status === 'SHORT'">
            <el-button v-if="!row.purchasable && canWO" size="small" @click="openWO(row)">開半成品製令</el-button>
            <span v-else-if="row.purchasable && !canPurchase" class="muted small">待採購處理</span>
          </template>
        </template>
      </el-table-column>
    </el-table>
    <p class="muted small legend">淨缺口 = 未扣需求 ×（1 + 預警緩衝%）− 庫存 − 在途；緩衝在產品資料設定（預設 3%）。</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, fmt } from '../../api'
import { categoryName } from '../../labels'
import { useSession } from '../../stores/session'
import { statusText } from '../dashboard/types'

interface Shortage {
  productId: string; itemNo: string; itemName: string; unit: string; category: string; need: number; onHand: number
  inboundWo: number; inboundPo: number; purchasable: boolean; supplier: string | null; shortage: number; status: 'OK' | 'COVERED' | 'SHORT'
  workOrders: { woNo: string; status: string; qty: number; dueDate: string | null }[]
}

const emit = defineEmits<{ changed: [] }>()
const session = useSession()
const canPurchase = computed(() => session.can('purchase:edit'))
const canWO = computed(() => session.can('wo:edit'))
const list = ref<Shortage[]>([])
const selected = ref<Shortage[]>([])
const filter = ref<'short' | 'all'>('short')
const busy = ref(false)
const statusName: Record<string, string> = { OK: '庫存足夠', COVERED: '在途可補足', SHORT: '缺料' }
const counts = computed(() => ({ SHORT: list.value.filter((s) => s.status === 'SHORT').length }))
const shown = computed(() => (filter.value === 'short' ? list.value.filter((s) => s.status === 'SHORT') : list.value))
const q = (v: number) => fmt.dec(v, v % 1 === 0 ? 0 : 2)

async function load() {
  list.value = await api<Shortage[]>('/shortages')
  if (!counts.value.SHORT && list.value.length) filter.value = 'all'
}
async function purchase() {
  busy.value = true
  try {
    const res = await api<{ itemNo: string; qty: number; poNo?: string; skip?: string }[]>('/shortages/purchase', {
      method: 'POST', body: { productIds: selected.value.map((s) => s.productId) },
    })
    const done = res.filter((r) => r.poNo)
    const pos = [...new Set(done.map((r) => r.poNo))]
    ElMessage({ type: done.length ? 'success' : 'info', duration: 5000,
      message: done.length ? `已加入採購單草稿 ${pos.join('、')}：${done.map((r) => `${r.itemNo} ${r.qty}`).join('、')}。請到「採購單」確認後下單。` : '沒有需要請購的項目' })
    await load()
    emit('changed')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    busy.value = false
  }
}
async function openWO(s: Shortage) {
  try {
    await ElMessageBox.confirm(`依目前缺口 ${q(s.shortage)} ${s.unit} 為 ${s.itemNo} 建立草稿製令（取整到每卡數量）？`, '開半成品製令', {
      confirmButtonText: '建立', cancelButtonText: '取消',
    })
    const r = await api<{ woNo: string; qty: number }>('/shortages/work-order', { method: 'POST', body: { productId: s.productId } })
    ElMessage.success(`已建立草稿製令 ${r.woNo}（${fmt.int(r.qty)} ${s.unit}），請到「訂單與製令」開立`)
    await load()
    emit('changed')
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error((e as Error).message)
  }
}
defineExpose({ load })
onMounted(load)
</script>

<style scoped>
.bar { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; flex-wrap: wrap; }
.spacer { flex: 1; }
.small { font-size: 12px; }
.bad { color: var(--div-neg-ink); }
.wos { padding: 4px 16px 8px 48px; font-size: 13px; line-height: 1.8; }
.status { display: inline-flex; align-items: center; gap: 6px; }
.status i { width: 8px; height: 8px; border-radius: 50%; }
.status.OK i { background: var(--status-good); }
.status.COVERED i { background: var(--status-warning); }
.status.SHORT i { background: var(--status-critical); }
.status.SHORT { font-weight: 600; }
.legend { margin: 10px 0 0; }
</style>
