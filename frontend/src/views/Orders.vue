<template>
  <div class="page">
    <div class="page-title"><h2>訂單與製令</h2></div>
    <div class="card">
      <el-tabs v-model="tab">
        <!-- 客戶訂單 -->
        <el-tab-pane label="客戶訂單" name="so">
          <div class="bar">
            <el-radio-group v-model="soFilter" size="small">
              <el-radio-button value="open">未結案</el-radio-button>
              <el-radio-button value="all">全部</el-radio-button>
            </el-radio-group>
            <el-button v-if="canEdit" type="primary" @click="openSO(null)">新增訂單</el-button>
          </div>
          <el-table :data="shownOrders" size="small" empty-text="沒有訂單">
            <el-table-column label="訂單" min-width="180">
              <template #default="{ row }"><b>{{ row.soNo }}</b><div class="muted small">PO：{{ row.customerPo ?? '—' }}</div></template>
            </el-table-column>
            <el-table-column label="客戶" prop="customer" min-width="140" />
            <el-table-column label="訂單日" width="100"><template #default="{ row }"><span class="num">{{ fmt.date(row.orderDate) }}</span></template></el-table-column>
            <el-table-column label="狀態" width="86">
              <template #default="{ row }"><el-tag size="small" :type="soTag[row.status]">{{ soStatus[row.status] }}</el-tag></template>
            </el-table-column>
            <el-table-column label="明細 → 製令" min-width="420">
              <template #default="{ row }">
                <div v-for="l in row.lines" :key="l.lineNo" class="so-line">
                  <span>{{ l.lineNo }}. {{ l.itemNo }} {{ l.itemName }}</span>
                  <span class="num">{{ fmt.int(l.qty) }} 雙</span>
                  <span class="muted num">交期 {{ fmt.date(l.dueDate) }}</span>
                  <span v-if="l.woNo">→ {{ l.woNo }} <span class="muted">（{{ statusText[l.woStatus!] }}，完工 {{ fmt.int(l.finishedQty) }}）</span></span>
                  <span v-else-if="row.status !== 'CANCELLED'" class="warn">未轉製令</span>
                </div>
              </template>
            </el-table-column>
            <el-table-column v-if="canEdit" label="操作" width="190">
              <template #default="{ row }">
                <template v-if="row.status !== 'CANCELLED'">
                  <el-button v-if="row.lines.some((l: any) => !l.woNo)" size="small" type="primary" @click="convert(row)">轉製令</el-button>
                  <el-button v-if="!row.lines.some((l: any) => l.woNo)" size="small" @click="openSO(row)">修改</el-button>
                  <el-button v-if="!row.lines.some((l: any) => l.woNo)" size="small" text type="danger" @click="cancelSO(row)">取消</el-button>
                </template>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <!-- 製令單 -->
        <el-tab-pane label="製令單" name="wo">
          <div class="bar">
            <el-radio-group v-model="woFilter" size="small">
              <el-radio-button value="open">未結案</el-radio-button>
              <el-radio-button value="all">全部</el-radio-button>
            </el-radio-group>
            <el-button v-if="canEdit" type="primary" @click="openWO(null)">新增製令</el-button>
          </div>
          <el-table :data="shownWOs" size="small" empty-text="沒有製令">
            <el-table-column label="製令" min-width="180">
              <template #default="{ row }">
                <b>{{ row.woNo }}</b>
                <div class="small">
                  <el-tag size="small" :type="woTag[row.status]">{{ statusText[row.status] }}</el-tag>
                  <el-tag v-if="row.source === 'SHORTAGE'" size="small" type="warning" class="ml">缺料衍生</el-tag>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="品項" min-width="160">
              <template #default="{ row }">{{ row.itemNo }}<div class="muted small">{{ row.itemName }}</div></template>
            </el-table-column>
            <el-table-column label="來源" min-width="160">
              <template #default="{ row }">
                <template v-if="row.soNo">{{ row.soNo }}<div class="muted small">{{ row.customer }}</div></template>
                <template v-else-if="row.parentWoNo">缺料支援 {{ row.parentWoNo }}</template>
                <span v-else class="muted">手動</span>
              </template>
            </el-table-column>
            <el-table-column label="交期" width="96"><template #default="{ row }"><span class="num">{{ fmt.date(row.dueDate) }}</span></template></el-table-column>
            <el-table-column label="各站（良品／投入）" min-width="190">
              <template #default="{ row }">
                <div v-for="s in row.steps" :key="s.seq" class="num small">{{ s.station }}：{{ fmt.int(s.good) }}／{{ fmt.int(s.input) }}</div>
              </template>
            </el-table-column>
            <el-table-column label="完工／計畫" width="110" align="right">
              <template #default="{ row }"><span class="num">{{ fmt.int(row.finishedQty) }}／{{ fmt.int(row.plannedQty) }}</span></template>
            </el-table-column>
            <el-table-column label="操作" width="230">
              <template #default="{ row }">
                <div class="ops">
                  <el-button v-if="row.status === 'DRAFT' && session.can('wo:release')" size="small" type="primary" @click="release([row])">開立</el-button>
                  <el-button v-if="row.status === 'DRAFT' && canEdit" size="small" @click="openWO(row)">修改</el-button>
                  <el-button v-if="['RELEASED', 'IN_PROGRESS'].includes(row.status) && session.can('inventory:edit')" size="small" @click="issue(row.id)">發料</el-button>
                  <el-button v-if="row.cards && row.status !== 'CANCELLED'" size="small" @click="openCards(row)">流程卡</el-button>
                  <el-button v-if="['DRAFT', 'RELEASED'].includes(row.status) && canEdit" size="small" text type="danger" @click="cancelWO(row)">取消</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <!-- 客戶 -->
        <el-tab-pane label="客戶" name="customers">
          <div class="bar"><span /><el-button v-if="canEdit" type="primary" @click="openCustomer(null)">新增客戶</el-button></div>
          <el-table :data="customers" size="small" empty-text="尚無客戶">
            <el-table-column label="代碼" prop="code" width="110" />
            <el-table-column label="名稱" prop="name" min-width="200" />
            <el-table-column label="訂單數" prop="orders" width="90" align="right" />
            <el-table-column label="狀態" width="90">
              <template #default="{ row }"><el-tag size="small" :type="row.isActive ? 'success' : 'info'">{{ row.isActive ? '往來中' : '停用' }}</el-tag></template>
            </el-table-column>
            <el-table-column v-if="canEdit" width="80"><template #default="{ row }"><el-button size="small" text @click="openCustomer(row)">編輯</el-button></template></el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </div>

    <CardsDialog v-model="cardsDlg.open" :wo-id="cardsDlg.id" :wo-no="cardsDlg.woNo" @changed="load" />
    <SalesOrderDialog v-model="soDlg.open" :order="soDlg.order" :customers="customers" :products="products" @saved="load" />

    <el-dialog v-model="woDlg.open" :title="woDlg.id ? `修改製令 ${woDlg.woNo}` : '新增製令（不經訂單）'" width="min(460px, 94vw)">
      <el-form label-width="80px" @submit.prevent>
        <el-form-item label="品項" required>
          <el-select v-model="woDlg.form.productId" filterable :disabled="!!woDlg.id" placeholder="成品或半成品" class="w-full">
            <el-option v-for="p in madeProducts" :key="p.id" :label="`${p.itemNo} ${p.name}`" :value="p.id" :disabled="!p.activeRoutingVersion">
              <span>{{ p.itemNo }} {{ p.name }}</span>
              <span class="opt-r">{{ p.activeRoutingVersion ? `途程 v${p.activeRoutingVersion}` : '無途程，不可開製令' }}</span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="數量" required><el-input-number v-model="woDlg.form.qty" :min="1" :step="100" /></el-form-item>
        <el-form-item label="交期">
          <el-date-picker v-model="woDlg.form.dueDate" type="date" value-format="YYYY-MM-DD" placeholder="可不填" />
        </el-form-item>
        <p v-if="!woDlg.id" class="muted small">建立後為草稿，按「開立」才會產生流程卡並檢查用料。</p>
      </el-form>
      <template #footer>
        <el-button @click="woDlg.open = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="saveWO">{{ woDlg.id ? '儲存' : '建立草稿' }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="custDlg.open" :title="custDlg.id ? '編輯客戶' : '新增客戶'" width="min(420px, 94vw)">
      <el-form label-width="70px" @submit.prevent>
        <el-form-item label="代碼" required><el-input v-model="custDlg.form.code" maxlength="20" /></el-form-item>
        <el-form-item label="名稱" required><el-input v-model="custDlg.form.name" maxlength="100" /></el-form-item>
        <el-form-item label="往來中"><el-switch v-model="custDlg.form.isActive" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="custDlg.open = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="saveCustomer">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, fmt } from '../api'
import { useSession } from '../stores/session'
import { statusText, type WorkOrder } from '../components/dashboard/types'
import SalesOrderDialog from '../components/orders/SalesOrderDialog.vue'
import CardsDialog from '../components/orders/CardsDialog.vue'
import type { Customer, ProductLite, SalesOrder } from '../components/orders/types'

const session = useSession()
const canEdit = computed(() => session.can('wo:edit'))
const tab = ref('so')
const busy = ref(false)
const orders = ref<SalesOrder[]>([])
const workOrders = ref<WorkOrder[]>([])
const customers = ref<Customer[]>([])
const products = ref<ProductLite[]>([])
const soFilter = ref<'open' | 'all'>('open')
const woFilter = ref<'open' | 'all'>('open')
const soStatus: Record<string, string> = { OPEN: '未生產', IN_PRODUCTION: '生產中', COMPLETED: '已完成', CLOSED: '結案', CANCELLED: '已取消' }
const soTag: Record<string, string> = { OPEN: 'info', IN_PRODUCTION: 'primary', COMPLETED: 'success', CANCELLED: 'info' }
const woTag: Record<string, string> = { DRAFT: 'info', RELEASED: 'warning', IN_PROGRESS: 'primary', COMPLETED: 'success', CANCELLED: 'info' }

const shownOrders = computed(() => orders.value.filter((o) => soFilter.value === 'all' || !['COMPLETED', 'CLOSED', 'CANCELLED'].includes(o.status)))
const shownWOs = computed(() => workOrders.value.filter((w) => woFilter.value === 'all' || !['COMPLETED', 'CLOSED', 'CANCELLED'].includes(w.status)))
const madeProducts = computed(() => products.value.filter((p) => (p.category === 'FG' || p.category === 'SFG') && p.isActive))

const soDlg = reactive({ open: false, order: null as SalesOrder | null })
const woDlg = reactive({ open: false, id: '', woNo: '', form: { productId: '', qty: 100, dueDate: '' as string | null } })
const custDlg = reactive({ open: false, id: '', form: { code: '', name: '', isActive: true } })

async function load() {
  ;[orders.value, workOrders.value, customers.value, products.value] = await Promise.all([
    api<SalesOrder[]>('/sales-orders'),
    api<WorkOrder[]>('/work-orders'),
    api<Customer[]>('/customers'),
    session.can('product:view') ? api<ProductLite[]>('/products') : Promise.resolve([]),
  ])
}
function fail(e: unknown) {
  if (e !== 'cancel' && e !== 'close') ElMessage.error((e as Error).message)
}
// 通知頂端列重新檢查缺料預警（會彈窗）
const alertsChanged = () => window.dispatchEvent(new Event('mes:alerts-changed'))

function openSO(o: SalesOrder | null) {
  soDlg.order = o
  soDlg.open = true
}
async function cancelSO(o: SalesOrder) {
  try {
    await ElMessageBox.confirm(`取消訂單 ${o.soNo}？`, '取消訂單', { confirmButtonText: '取消訂單', cancelButtonText: '返回', type: 'warning' })
    await api(`/sales-orders/${o.id}/cancel`, { method: 'POST' })
    ElMessage.success('訂單已取消')
    await load()
  } catch (e) {
    fail(e)
  }
}

async function convert(o: SalesOrder) {
  try {
    const created = await api<{ id: string; woNo: string }[]>(`/sales-orders/${o.id}/work-orders`, { method: 'POST' })
    await load()
    const canRelease = session.can('wo:release')
    if (!canRelease) {
      ElMessage.success(`已建立 ${created.length} 張草稿製令，請生管開立`)
      return
    }
    await ElMessageBox.confirm(
      `已建立 ${created.length} 張草稿製令：${created.map((w) => w.woNo).join('、')}。要立即開立嗎？開立會產生流程卡並檢查用料。`,
      '轉製令完成', { confirmButtonText: '立即開立', cancelButtonText: '稍後再開', type: 'success' },
    )
    await release(created.map((w) => ({ id: w.id, woNo: w.woNo })))
  } catch (e) {
    fail(e)
  }
}

async function release(list: { id: string; woNo: string }[]) {
  let cards = 0
  let alerts = 0
  const done: string[] = []
  const pos: string[] = []
  for (const w of list) {
    try {
      const r = await api<{ cards: number; alerts: string[]; purchaseOrders: string[] }>(`/work-orders/${w.id}/release`, { method: 'POST' })
      cards += r.cards
      alerts += r.alerts?.length ?? 0
      for (const po of r.purchaseOrders ?? []) if (!pos.includes(po)) pos.push(po)
      done.push(w.woNo)
    } catch (e) {
      ElMessage.error(`${w.woNo}：${(e as Error).message}`)
    }
  }
  await load()
  if (!done.length) return
  ElMessage({ type: alerts ? 'warning' : 'success', duration: 5000,
    message: `已開立 ${done.length} 張製令、流程卡 ${cards} 張${alerts ? `；缺料預警 ${alerts} 筆` : ''}${pos.length ? `；材料缺料已自動加入採購單 ${pos.join('、')}` : ''}` })
  if (alerts) alertsChanged()
  else tab.value = 'wo'
}

function openWO(w: WorkOrder | null) {
  woDlg.id = w?.id ?? ''
  woDlg.woNo = w?.woNo ?? ''
  const p = w && products.value.find((x) => x.itemNo === w.itemNo)
  woDlg.form = w ? { productId: p?.id ?? '', qty: w.plannedQty, dueDate: w.dueDate?.slice(0, 10) ?? null } : { productId: '', qty: 100, dueDate: null }
  woDlg.open = true
}
async function saveWO() {
  if (!woDlg.form.productId) return ElMessage.error('請選擇品項')
  busy.value = true
  try {
    const body = { ...woDlg.form, dueDate: woDlg.form.dueDate ?? '' }
    if (woDlg.id) {
      await api(`/work-orders/${woDlg.id}`, { method: 'PUT', body })
      ElMessage.success('已儲存')
    } else {
      const r = await api<{ woNo: string }>('/work-orders', { method: 'POST', body })
      ElMessage.success(`已建立草稿製令 ${r.woNo}`)
    }
    woDlg.open = false
    tab.value = 'wo'
    await load()
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}
async function cancelWO(w: WorkOrder) {
  try {
    const { value } = await ElMessageBox.prompt(
      `取消製令 ${w.woNo}？${w.status === 'RELEASED' ? '已產生的流程卡會作廢，相關缺料預警會自動結案。' : ''}`,
      '取消製令', { confirmButtonText: '取消製令', cancelButtonText: '返回', inputPlaceholder: '取消原因（選填）', type: 'warning' },
    )
    await api(`/work-orders/${w.id}/cancel`, { method: 'POST', body: { reason: value ?? '' } })
    ElMessage.success('製令已取消')
    alertsChanged()
    await load()
  } catch (e) {
    fail(e)
  }
}
async function issue(id: string) {
  try {
    type Line = { itemNo: string; unit: string; qty: number; stock?: number }
    const r = await api<{ issued: Line[]; short: Line[]; onArrival: string[] }>(`/work-orders/${id}/issue`, { method: 'POST' })
    const parts: string[] = []
    if (r.issued.length) parts.push(`已發料：${r.issued.map((l) => `${l.itemNo} ${fmt.dec(l.qty, l.qty % 1 ? 2 : 0)}`).join('、')}`)
    if (r.short.length) parts.push(`庫存不足已依現有量調整，仍缺：${r.short.map((l) => `${l.itemNo} ${fmt.dec(l.qty, l.qty % 1 ? 2 : 0)} ${l.unit}`).join('、')}（可到「缺料預警」產生採購單）`)
    if (!r.issued.length && !r.short.length) parts.push('出庫扣帳料件都已發完')
    if (r.onArrival.length) parts.push(`${r.onArrival.join('、')} 為到站扣帳，現場掃描時自動扣，不需發料`)
    ElMessage({ type: r.short.length ? 'warning' : 'success', duration: 7000, message: parts.join('。') })
    await load()
  } catch (e) {
    fail(e)
  }
}
const cardsDlg = reactive({ open: false, id: '', woNo: '' })
function openCards(w: WorkOrder) {
  Object.assign(cardsDlg, { open: true, id: w.id, woNo: w.woNo })
}

function openCustomer(c: Customer | null) {
  custDlg.id = c?.id ?? ''
  custDlg.form = c ? { code: c.code, name: c.name, isActive: c.isActive } : { code: '', name: '', isActive: true }
  custDlg.open = true
}
async function saveCustomer() {
  busy.value = true
  try {
    await api(custDlg.id ? `/customers/${custDlg.id}` : '/customers', { method: custDlg.id ? 'PUT' : 'POST', body: custDlg.form })
    ElMessage.success('已儲存')
    custDlg.open = false
    await load()
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.bar { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 10px; flex-wrap: wrap; }
.small { font-size: 12px; }
.ml { margin-left: 4px; }
.so-line { display: flex; flex-wrap: wrap; gap: 4px 12px; line-height: 1.8; }
.warn { color: var(--div-neg-ink); }
.ops { display: flex; flex-wrap: wrap; gap: 4px; }
.ops .el-button { margin-left: 0; }
.w-full { width: 100%; }
.opt-r { float: right; color: var(--text-muted); font-size: 12px; margin-left: 12px; }
</style>
