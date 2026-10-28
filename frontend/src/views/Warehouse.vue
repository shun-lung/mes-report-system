<template>
  <div class="page">
    <div class="page-title"><h2>倉儲作業</h2><span class="muted">所有作業都會留下異動歷程；打錯請到「庫存與歷程」沖銷</span></div>

    <el-empty v-if="!canEdit && !canAdjust" description="此帳號沒有入出庫權限" />
    <div v-else class="card">
      <el-tabs v-model="tab">
        <el-tab-pane v-if="canEdit" label="收料入庫" name="receipt">
          <el-form label-width="80px" @submit.prevent>
            <el-form-item label="入庫倉"><WarehouseSelect v-model="receipt.warehouseId" :warehouses="warehouses" /></el-form-item>
            <el-form-item label="說明"><el-input v-model="receipt.note" maxlength="200" placeholder="例如：供應商、進貨單號" /></el-form-item>
          </el-form>
          <LineEditor v-model="receipt.rows" :products="products" :stock-of="(id) => stockOf(receipt.warehouseId, id)" />
          <div class="submit"><el-button type="primary" :loading="busy" @click="submit('receipt')">確認入庫</el-button></div>
        </el-tab-pane>

        <el-tab-pane v-if="canEdit" label="製令發料" name="woIssue">
          <el-form label-width="80px" @submit.prevent>
            <el-form-item label="製令">
              <el-select v-model="woIssue.workOrderId" filterable placeholder="選擇已開立的製令" class="w-wo" @change="loadMaterials">
                <el-option v-for="w in openWOs" :key="w.id" :label="`${w.woNo}　${w.itemNo} ${w.itemName}`" :value="w.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="發料倉"><WarehouseSelect v-model="woIssue.warehouseId" :warehouses="warehouses" /></el-form-item>
            <el-form-item label="說明"><el-input v-model="woIssue.note" maxlength="200" placeholder="例如：補料原因" /></el-form-item>
          </el-form>
          <template v-if="woIssue.workOrderId">
            <div class="bar">
              <span class="muted small">「到站扣帳」料件會在現場掃描時自動扣帳，不能在此發料（避免重複扣）。庫存不足時會依現有量帶入。</span>
              <el-button size="small" @click="fillRemaining">依未發數量帶入</el-button>
            </div>
            <el-table :data="materials" size="small">
              <el-table-column label="料件" min-width="180">
                <template #default="{ row }"><b>{{ row.itemNo }}</b> {{ row.itemName }}</template>
              </el-table-column>
              <el-table-column label="扣帳" width="120">
                <template #default="{ row }">{{ timingName[row.deductTiming] }}<span v-if="row.consumeStationName" class="muted"> · {{ row.consumeStationName }}</span></template>
              </el-table-column>
              <el-table-column label="需求" width="90" align="right"><template #default="{ row }"><span class="num">{{ q(row.requiredQty) }}</span></template></el-table-column>
              <el-table-column label="已發" width="90" align="right"><template #default="{ row }"><span class="num">{{ q(row.issuedQty) }}</span></template></el-table-column>
              <el-table-column label="未發" width="90" align="right">
                <template #default="{ row }"><span class="num" :class="{ warn: remainingOf(row) > 0 }">{{ q(remainingOf(row)) }}</span></template>
              </el-table-column>
              <el-table-column label="此倉現有" width="110" align="right">
                <template #default="{ row }">
                  <span class="num" :class="{ warn: row.deductTiming === 'ON_ISSUE' && stockOf(woIssue.warehouseId, row.productId) < remainingOf(row) }">
                    {{ q(stockOf(woIssue.warehouseId, row.productId)) }}</span> {{ row.unit }}
                  <div v-if="otherStock(row)" class="muted small">{{ otherStock(row) }}</div>
                </template>
              </el-table-column>
              <el-table-column label="本次發料" width="170">
                <template #default="{ row }">
                  <span v-if="row.deductTiming !== 'ON_ISSUE'" class="muted small">現場掃描自動扣帳</span>
                  <el-input-number v-else v-model="woIssue.qty[row.productId]" :min="0" controls-position="right" class="w-full" />
                </template>
              </el-table-column>
            </el-table>
            <div class="submit"><el-button type="primary" :loading="busy" @click="submit('woIssue')">確認發料</el-button></div>
          </template>
        </el-tab-pane>

        <el-tab-pane v-if="canEdit" label="一般發料" name="issue">
          <el-form label-width="80px" @submit.prevent>
            <el-form-item label="發料倉"><WarehouseSelect v-model="issue.warehouseId" :warehouses="warehouses" /></el-form-item>
            <el-form-item label="用途" required><el-input v-model="issue.note" maxlength="200" placeholder="例如：打樣、設備維修" /></el-form-item>
          </el-form>
          <LineEditor v-model="issue.rows" :products="products" :stock-of="(id) => stockOf(issue.warehouseId, id)" />
          <div class="submit"><el-button type="primary" :loading="busy" @click="submit('issue')">確認發料</el-button></div>
        </el-tab-pane>

        <el-tab-pane v-if="canEdit" label="倉庫移轉" name="transfer">
          <el-form label-width="80px" @submit.prevent>
            <el-form-item label="從">
              <WarehouseSelect v-model="transfer.fromWarehouseId" :warehouses="warehouses" />
              <span class="arrow">→</span>
              <WarehouseSelect v-model="transfer.toWarehouseId" :warehouses="warehouses" />
            </el-form-item>
            <el-form-item label="說明"><el-input v-model="transfer.note" maxlength="200" /></el-form-item>
          </el-form>
          <LineEditor v-model="transfer.rows" :products="products" :stock-of="(id) => stockOf(transfer.fromWarehouseId, id)" />
          <div class="submit"><el-button type="primary" :loading="busy" @click="submit('transfer')">確認移轉</el-button></div>
        </el-tab-pane>

        <el-tab-pane v-if="canAdjust" label="盤點調整" name="adjust">
          <el-form label-width="80px" @submit.prevent>
            <el-form-item label="盤點倉">
              <WarehouseSelect v-model="adjust.warehouseId" :warehouses="warehouses" />
              <el-button class="ml" @click="loadAllForCount">帶入此倉所有料件</el-button>
            </el-form-item>
            <el-form-item label="說明"><el-input v-model="adjust.note" maxlength="200" placeholder="例如：10 月月底盤點" /></el-form-item>
          </el-form>
          <p class="muted small">輸入實盤數量，系統依「實盤 − 帳面」產生調整；無差異的料件不會產生紀錄。</p>
          <LineEditor v-model="adjust.rows" mode="count" :products="products" :stock-of="(id) => stockOf(adjust.warehouseId, id)" />
          <div class="submit"><el-button type="primary" :loading="busy" @click="submit('adjust')">確認調整</el-button></div>
        </el-tab-pane>

        <el-tab-pane v-if="canAdjust" label="倉庫設定" name="warehouses">
          <div class="bar"><span /><el-button size="small" type="primary" @click="editWh()">新增倉庫</el-button></div>
          <el-table :data="warehouses" size="small">
            <el-table-column label="代碼" prop="code" width="90" />
            <el-table-column label="名稱" prop="name" min-width="140" />
            <el-table-column label="類型" width="110"><template #default="{ row }">{{ whTypeName[row.type] }}</template></el-table-column>
            <el-table-column label="對應站點" width="110"><template #default="{ row }">{{ row.stationName ?? '—' }}</template></el-table-column>
            <el-table-column label="有庫存料件" prop="items" width="100" align="right" />
            <el-table-column label="狀態" width="80">
              <template #default="{ row }"><el-tag size="small" :type="row.isActive ? 'success' : 'info'">{{ row.isActive ? '使用中' : '停用' }}</el-tag></template>
            </el-table-column>
            <el-table-column width="70"><template #default="{ row }"><el-button size="small" text @click="editWh(row)">編輯</el-button></template></el-table-column>
          </el-table>
          <p class="muted small">系統自動扣帳與入庫時，材料用「原物料」、半成品用「暫存」、成品用「成品」類型中代碼排序最前、使用中的倉庫。</p>
        </el-tab-pane>
      </el-tabs>
    </div>

    <el-dialog v-model="whDlg.open" :title="whDlg.id ? '編輯倉庫' : '新增倉庫'" width="min(440px, 94vw)">
      <el-form label-width="80px" @submit.prevent>
        <el-form-item label="代碼" required><el-input v-model="whDlg.form.code" maxlength="20" /></el-form-item>
        <el-form-item label="名稱" required><el-input v-model="whDlg.form.name" maxlength="100" /></el-form-item>
        <el-form-item label="類型">
          <el-radio-group v-model="whDlg.form.type">
            <el-radio-button v-for="(v, k) in whTypeName" :key="k" :value="k">{{ v }}</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="對應站點">
          <el-select v-model="whDlg.form.stationId" clearable placeholder="暫存倉可指定站點">
            <el-option v-for="s in stations" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="使用中"><el-switch v-model="whDlg.form.isActive" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="whDlg.open = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="saveWh">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, fmt } from '../api'
import { timingName } from '../labels'
import { useSession } from '../stores/session'
import LineEditor, { type ProductOpt, type Row } from '../components/warehouse/LineEditor.vue'
import WarehouseSelect, { type WH } from '../components/warehouse/WarehouseSelect.vue'

interface Balance { warehouseCode: string; productId: string; qty: number }
interface Material { id: string; productId: string; itemNo: string; itemName: string; unit: string; deductTiming: string; consumeStationName: string | null; requiredQty: number; issuedQty: number }
interface WO { id: string; woNo: string; status: string; itemNo: string; itemName: string }

const session = useSession()
const canEdit = computed(() => session.can('inventory:edit'))
const canAdjust = computed(() => session.can('inventory:adjust'))
const tab = ref(canEdit.value ? 'receipt' : 'adjust')
const busy = ref(false)
const warehouses = ref<WH[]>([])
const products = ref<ProductOpt[]>([])
const balances = ref<Balance[]>([])
const stations = ref<{ id: string; name: string }[]>([])
const openWOs = ref<WO[]>([])
const materials = ref<Material[]>([])
const whTypeName: Record<string, string> = { RAW: '原物料', WIP: '暫存（半成品）', FG: '成品' }

const firstOf = (type: string) => warehouses.value.find((w) => w.type === type && w.isActive)?.id ?? ''
const receipt = reactive({ warehouseId: '', note: '', rows: [] as Row[] })
const issue = reactive({ warehouseId: '', note: '', rows: [] as Row[] })
const transfer = reactive({ fromWarehouseId: '', toWarehouseId: '', note: '', rows: [] as Row[] })
const adjust = reactive({ warehouseId: '', note: '', rows: [] as Row[] })
const woIssue = reactive({ workOrderId: '', warehouseId: '', note: '', qty: {} as Record<string, number> })
const whDlg = reactive({ open: false, id: '', form: { code: '', name: '', type: 'RAW', stationId: null as string | null, isActive: true } })

const q = (v: number) => fmt.dec(v, v % 1 === 0 ? 0 : 2)
const remainingOf = (m: Material) => Math.max(0, Math.round((m.requiredQty - m.issuedQty) * 10000) / 10000)
function stockOf(warehouseId: string, productId: string) {
  const code = warehouses.value.find((w) => w.id === warehouseId)?.code
  return balances.value.find((b) => b.warehouseCode === code && b.productId === productId)?.qty ?? 0
}

async function loadBase() {
  const [w, p, b, s, wos] = await Promise.all([
    api<WH[]>('/warehouses'),
    api<ProductOpt[]>('/products').catch(() => []),
    api<Balance[]>('/inventory/balances'),
    session.can('station:view') ? api<{ id: string; name: string }[]>('/stations') : Promise.resolve([]),
    api<WO[]>('/work-orders').catch(() => []),
  ])
  warehouses.value = w
  products.value = p.filter((x) => x.isActive)
  balances.value = b
  stations.value = s
  openWOs.value = wos.filter((x) => x.status === 'RELEASED' || x.status === 'IN_PROGRESS')
  receipt.warehouseId ||= firstOf('RAW')
  issue.warehouseId ||= firstOf('RAW')
  woIssue.warehouseId ||= firstOf('RAW')
  transfer.fromWarehouseId ||= firstOf('RAW')
  transfer.toWarehouseId ||= firstOf('WIP')
  adjust.warehouseId ||= firstOf('RAW')
}
async function loadMaterials() {
  materials.value = await api<Material[]>(`/inventory/work-orders/${woIssue.workOrderId}/materials`)
  woIssue.qty = Object.fromEntries(materials.value.map((m) => [m.productId, 0]))
}
// 依未發量帶入；此倉庫存不足時改帶現有量並提示
function fillRemaining() {
  const short: string[] = []
  for (const m of materials.value) {
    if (m.deductTiming !== 'ON_ISSUE') continue
    const stock = Math.max(0, stockOf(woIssue.warehouseId, m.productId))
    const need = remainingOf(m)
    woIssue.qty[m.productId] = Math.min(need, stock)
    if (stock < need) short.push(`${m.itemNo} 缺 ${q(need - stock)} ${m.unit}`)
  }
  if (short.length) ElMessage.warning({ message: `此倉庫存不足，已依現有量帶入：${short.join('、')}`, duration: 6000 })
  else if (!materials.value.some((m) => m.deductTiming === 'ON_ISSUE' && remainingOf(m) > 0)) ElMessage.info('出庫扣帳料件都已發完')
}
// 其他倉有庫存時提示（例如半成品在暫存倉）
function otherStock(m: Material) {
  const here = warehouses.value.find((w) => w.id === woIssue.warehouseId)?.code
  const other = balances.value.filter((b) => b.productId === m.productId && b.warehouseCode !== here && b.qty > 0)
  return other.length ? '其他倉：' + other.map((b) => `${b.warehouseCode} ${q(b.qty)}`).join('、') : ''
}
function loadAllForCount() {
  const code = warehouses.value.find((w) => w.id === adjust.warehouseId)?.code
  adjust.rows = balances.value.filter((b) => b.warehouseCode === code).map((b) => ({ productId: b.productId, qty: b.qty }))
}

const validRows = (rows: Row[], allowZero = false) => rows.filter((r) => r.productId && (allowZero ? r.qty >= 0 : r.qty > 0))

async function submit(kind: 'receipt' | 'issue' | 'woIssue' | 'transfer' | 'adjust') {
  let path = ''
  let body: Record<string, unknown> = {}
  if (kind === 'receipt') {
    path = '/inventory/receipts'
    body = { warehouseId: receipt.warehouseId, note: receipt.note, lines: validRows(receipt.rows).map((r) => ({ productId: r.productId, qty: r.qty })) }
  } else if (kind === 'issue') {
    if (!issue.note.trim()) return ElMessage.error('請填寫用途')
    path = '/inventory/issues'
    body = { warehouseId: issue.warehouseId, note: issue.note, lines: validRows(issue.rows).map((r) => ({ productId: r.productId, qty: r.qty })) }
  } else if (kind === 'woIssue') {
    path = '/inventory/issues'
    const onIssue = new Set(materials.value.filter((m) => m.deductTiming === 'ON_ISSUE').map((m) => m.productId))
    const lines = Object.entries(woIssue.qty).filter(([id, v]) => v > 0 && onIssue.has(id)).map(([productId, qty]) => ({ productId, qty }))
    body = { warehouseId: woIssue.warehouseId, workOrderId: woIssue.workOrderId, note: woIssue.note, lines }
  } else if (kind === 'transfer') {
    path = '/inventory/transfers'
    body = { ...transfer, rows: undefined, lines: validRows(transfer.rows).map((r) => ({ productId: r.productId, qty: r.qty })) }
  } else {
    path = '/inventory/adjustments'
    body = { warehouseId: adjust.warehouseId, note: adjust.note, lines: validRows(adjust.rows, true).map((r) => ({ productId: r.productId, countedQty: r.qty })) }
  }
  if (!(body.lines as unknown[]).length) return ElMessage.error('請至少輸入一個料件與數量')
  busy.value = true
  try {
    const res = await api<{ posted: number }>(path, { method: 'POST', body })
    ElMessage.success(res.posted ? `完成，產生 ${res.posted} 筆異動` : '帳面與實盤相同，沒有需要調整的項目')
    if (kind === 'receipt') Object.assign(receipt, { note: '', rows: [] })
    if (kind === 'issue') Object.assign(issue, { note: '', rows: [] })
    if (kind === 'transfer') Object.assign(transfer, { note: '', rows: [] })
    if (kind === 'adjust') Object.assign(adjust, { note: '', rows: [] })
    await loadBase()
    if (kind === 'woIssue') {
      woIssue.note = ''
      await loadMaterials()
    }
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    busy.value = false
  }
}

function editWh(w?: WH) {
  whDlg.id = w?.id ?? ''
  whDlg.form = w
    ? { code: w.code, name: w.name, type: w.type, stationId: w.stationId, isActive: w.isActive }
    : { code: '', name: '', type: 'RAW', stationId: null, isActive: true }
  whDlg.open = true
}
async function saveWh() {
  busy.value = true
  try {
    await api(whDlg.id ? `/warehouses/${whDlg.id}` : '/warehouses', { method: whDlg.id ? 'PUT' : 'POST', body: { ...whDlg.form, stationId: whDlg.form.stationId || null } })
    ElMessage.success('已儲存')
    whDlg.open = false
    await loadBase()
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    busy.value = false
  }
}

onMounted(loadBase)
</script>

<style scoped>
.submit { margin-top: 14px; display: flex; justify-content: flex-end; }
.bar { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 8px; flex-wrap: wrap; }
.small { font-size: 12px; }
.w-wo { width: min(420px, 100%); }
.w-full { width: 100%; }
.arrow { margin: 0 10px; color: var(--text-muted); }
.ml { margin-left: 8px; }
.warn { color: var(--div-neg-ink); font-weight: 600; }
</style>
