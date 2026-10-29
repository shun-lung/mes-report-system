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
            <span class="tools">
              <el-button v-if="canEdit" type="primary" @click="openSO(null)">新增訂單</el-button>
              <el-button v-if="canEdit" type="primary" plain :disabled="!selectedSO.length" @click="openConvert">
                轉製令{{ selectedSO.length ? `（${selectedSO.length}）` : '' }}
              </el-button>
            </span>
          </div>
          <el-table :data="shownOrders" size="small" empty-text="沒有訂單" @selection-change="(rows: SalesOrder[]) => (selectedSO = rows)">
            <el-table-column v-if="canEdit" type="selection" width="36" :selectable="convertible" />
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
            <div class="tools">
              <el-radio-group v-model="woFilter" size="small">
                <el-radio-button value="open">未結案</el-radio-button>
                <el-radio-button value="all">全部</el-radio-button>
              </el-radio-group>
              <el-input v-model="woSearch" size="small" clearable placeholder="搜尋製令、客戶、訂單、品項" class="search" />
              <el-select v-model="woStation" size="small" clearable placeholder="製程站（全部）" class="station-filter">
                <el-option v-for="n in stationNames" :key="n" :label="n" :value="n" />
              </el-select>
            </div>
            <div class="tools">
              <template v-if="canLabor">
                <el-button :disabled="!selectedWO.length" type="success" @click="openBatchLabor">整批上工（{{ selectedWO.length }}）</el-button>
                <el-button :disabled="!selectedRunning.length" @click="batchEnd">整批下工（{{ selectedRunning.length }}）</el-button>
              </template>
              <el-button v-if="canEdit" type="primary" @click="openWO(null)">新增製令</el-button>
            </div>
          </div>
          <el-table ref="woTable" :data="shownWOs" size="small" empty-text="沒有符合的製令" row-key="id"
            @selection-change="(rows: WorkOrder[]) => (selectedWO = rows)">
            <el-table-column v-if="canLabor" type="selection" width="36" reserve-selection :selectable="laborable" />
            <el-table-column label="製令" min-width="180">
              <template #default="{ row }">
                <b>{{ row.woNo }}</b>
                <div class="small">
                  <el-tag size="small" :type="woTag[row.status]">{{ statusText[row.status] }}</el-tag>
                  <el-tag v-if="row.source === 'SHORTAGE'" size="small" type="warning" class="ml">缺料衍生</el-tag>
                  <el-tag v-if="row.requireAuth" size="small" type="danger" class="ml" effect="plain">人員授權</el-tag>
                  <el-tag v-if="row.requireLabor" size="small" type="danger" class="ml" effect="plain">強制上工</el-tag>
                </div>
                <div v-for="l in laborOf(row.id)" :key="l.lineId" class="running small">● 上工中 {{ l.lineCode }} · {{ l.headcount }} 人</div>
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
            <el-table-column min-width="300">
              <template #header>
                全流程進度
                <span class="legend"><i class="dot s-wait" />未到站／未開始 <i class="dot s-run" />生產中 <i class="dot s-done" />完成</span>
              </template>
              <template #default="{ row }">
                <div class="flow">
                  <template v-for="(s, i) in flow(row)" :key="s.seq">
                    <span v-if="i" class="arrow">→</span>
                    <span class="step" :class="'s-' + s.state" :title="`${s.station}：已到 ${fmt.int(s.arrived)}，投入 ${fmt.int(s.input)}，良品 ${fmt.int(s.good)}，不良 ${fmt.int(s.defect)}`">
                      {{ s.station }}<small class="num">{{ fmt.int(s.good) }}／{{ fmt.int(s.arrived) }}<b v-if="s.defect" class="bad">×{{ fmt.int(s.defect) }}</b></small>
                    </span>
                  </template>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="發料" width="120">
              <template #default="{ row }">
                <template v-for="st in [issueState(row)]" :key="st.text">
                  <el-tag size="small" :type="st.type" :effect="st.type === 'danger' ? 'dark' : 'light'">{{ st.text }}</el-tag>
                  <div v-if="st.note" class="muted small">{{ st.note }}</div>
                </template>
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
                  <el-button v-if="['RELEASED', 'IN_PROGRESS'].includes(row.status) && session.can('inventory:edit') && row.issue.done < row.issue.items"
                    size="small" type="warning" @click="issue(row.id)">發料</el-button>
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

    <el-dialog v-model="laborDlg.open" title="整批上工" width="min(760px, 96vw)">
      <el-form inline @submit.prevent>
        <el-form-item label="上工站點">
          <el-select v-model="laborDlg.stationId" style="width: 160px" @change="autoAssign">
            <el-option v-for="st in laborStations" :key="st.id" :label="st.name" :value="st.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="每線人數">
          <el-input-number v-model="laborDlg.headcount" :min="1" :max="200" @change="(v: any) => laborDlg.rows.forEach((r) => (r.headcount = v))" />
        </el-form-item>
      </el-form>
      <p class="muted small">同一條線同時只能上工一張製令；系統先把空閒的線依序分配，可自行調整。若線上已有其他製令，會先下工再換上。</p>
      <el-table :data="laborDlg.rows" size="small">
        <el-table-column label="製令" min-width="170">
          <template #default="{ row }"><b>{{ row.woNo }}</b><div class="muted small">{{ row.itemNo }} {{ row.itemName }}</div></template>
        </el-table-column>
        <el-table-column label="線別" min-width="220">
          <template #default="{ row }">
            <el-select v-model="row.lineId" placeholder="選擇線別" style="width: 100%">
              <el-option v-for="l in dlgLines" :key="l.id" :value="l.id"
                :label="l.code + (busyLabel(l.id, row.woId) ? `（${busyLabel(l.id, row.woId)}）` : '')" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="人數" width="140">
          <template #default="{ row }"><el-input-number v-model="row.headcount" :min="1" :max="200" size="small" /></template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="laborDlg.open = false">取消</el-button>
        <el-button type="success" :loading="laborDlg.saving" @click="confirmBatchLabor">確認上工</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="convertDlg.open" title="轉製令" width="min(520px, 94vw)">
      <p>將 <b>{{ selectedSO.length }}</b> 張訂單中尚未轉製令的明細，各建立一張製令：</p>
      <ul class="so-list">
        <li v-for="o in selectedSO" :key="o.id">{{ o.soNo }}　{{ o.customer }}（{{ o.lines.filter((l) => !l.woNo).length }} 項）</li>
      </ul>
      <el-form label-width="80px" @submit.prevent>
        <el-form-item label="生管管制">
          <div class="ctl-opts">
            <el-checkbox v-model="convertDlg.requireAuth">人員授權<span class="muted small">：只有被授權該線別的人能回報</span></el-checkbox>
            <el-checkbox v-model="convertDlg.requireLabor">強制上工<span class="muted small">：須先上工此製令才能回報</span></el-checkbox>
            <span class="muted small">都不勾＝不管制（預設）</span>
          </div>
        </el-form-item>
        <el-form-item v-if="session.can('wo:release')" label="開立">
          <el-checkbox v-model="convertDlg.release">轉製令後立即開立（產生流程卡並檢查用料）</el-checkbox>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="convertDlg.open = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="convert">確認轉製令</el-button>
      </template>
    </el-dialog>

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
        <el-form-item label="生管管制">
          <el-checkbox v-model="woDlg.form.requireAuth">人員授權</el-checkbox>
          <el-checkbox v-model="woDlg.form.requireLabor">強制上工</el-checkbox>
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
const woSearch = ref('')
const woStation = ref('')
const stationNames = computed(() => [...new Set(workOrders.value.flatMap((w) => w.steps.map((s) => s.station)))])
// 搜尋：製令、訂單、客戶、品項；製程站：途程含該站的製令
const shownWOs = computed(() => {
  const q = woSearch.value.trim().toLowerCase()
  return workOrders.value.filter((w) =>
    (woFilter.value === 'all' || !['COMPLETED', 'CLOSED', 'CANCELLED'].includes(w.status)) &&
    (!woStation.value || w.steps.some((s) => s.station === woStation.value)) &&
    (!q || [w.woNo, w.soNo, w.customer, w.itemNo, w.itemName, w.parentWoNo].some((v) => v?.toLowerCase().includes(q))))
})
const madeProducts = computed(() => products.value.filter((p) => (p.category === 'FG' || p.category === 'SFG') && p.isActive))

const soDlg = reactive({ open: false, order: null as SalesOrder | null })
const woDlg = reactive({
  open: false, id: '', woNo: '',
  form: { productId: '', qty: 100, dueDate: '' as string | null, requireAuth: false, requireLabor: false },
})
const selectedSO = ref<SalesOrder[]>([])
const custDlg = reactive({ open: false, id: '', form: { code: '', name: '', isActive: true } })

async function load() {
  ;[orders.value, workOrders.value, customers.value, products.value, openLabor.value] = await Promise.all([
    api<SalesOrder[]>('/sales-orders'),
    api<WorkOrder[]>('/work-orders'),
    api<Customer[]>('/customers'),
    session.can('product:view') ? api<ProductLite[]>('/products') : Promise.resolve([]),
    api<OpenLabor[]>('/labor/open').catch(() => []),
  ])
}

// ── 整批上工／下工 ──
interface OpenLabor { lineId: string; lineCode: string; stationName: string; workOrderId: string; headcount: number }
interface LaborStation { id: string; name: string; lines: { id: string; code: string; isActive: boolean; stdHeadcount: number }[] }
const canLabor = computed(() => session.can('labor:report'))
const openLabor = ref<OpenLabor[]>([])
const selectedWO = ref<WorkOrder[]>([])
const woTable = ref()
const laborable = (w: WorkOrder) => ['RELEASED', 'IN_PROGRESS'].includes(w.status)
const laborOf = (woId: string) => openLabor.value.filter((l) => l.workOrderId === woId)
const selectedRunning = computed(() => selectedWO.value.filter((w) => laborOf(w.id).length))
const laborStations = ref<LaborStation[]>([])
const laborDlg = reactive({
  open: false, saving: false, stationId: '', headcount: 10,
  rows: [] as { woId: string; woNo: string; itemNo: string; itemName: string; lineId: string; headcount: number }[],
})
const dlgLines = computed(() => laborStations.value.find((s) => s.id === laborDlg.stationId)?.lines ?? [])
function busyLabel(lineId: string, woId: string) {
  const l = openLabor.value.find((x) => x.lineId === lineId)
  if (!l) return ''
  if (l.workOrderId === woId) return '已在上工'
  return '目前 ' + (workOrders.value.find((w) => w.id === l.workOrderId)?.woNo ?? '其他製令')
}
// 依序把空閒的線分配給各製令；已在此站上工的製令維持原線
function autoAssign() {
  const lines = dlgLines.value
  const used = new Set<string>()
  for (const r of laborDlg.rows) {
    const here = openLabor.value.find((l) => l.workOrderId === r.woId && lines.some((x) => x.id === l.lineId))
    r.lineId = here?.lineId ?? ''
    if (here) used.add(here.lineId)
  }
  const free = lines.filter((l) => !used.has(l.id) && !openLabor.value.some((o) => o.lineId === l.id))
  for (const r of laborDlg.rows) {
    if (r.lineId) continue
    const l = free.shift()
    if (l) { r.lineId = l.id; used.add(l.id) }
  }
  const std = lines.find((l) => l.stdHeadcount)?.stdHeadcount
  if (std) {
    laborDlg.headcount = std
    laborDlg.rows.forEach((r) => (r.headcount = std))
  }
}
async function openBatchLabor() {
  try {
    if (!laborStations.value.length) {
      laborStations.value = (await api<LaborStation[]>('/stations'))
        .map((s) => ({ ...s, lines: s.lines.filter((l) => l.isActive) })).filter((s) => s.lines.length)
    }
  } catch (e) {
    return fail(e)
  }
  // 預設站點：篩選中的製程站，否則選取製令共同的第一站
  const pick = woStation.value || selectedWO.value[0]?.steps.find((s) => selectedWO.value.every((w) => w.steps.some((x) => x.station === s.station)))?.station
  laborDlg.stationId = laborStations.value.find((s) => s.name === pick)?.id ?? laborStations.value[0]?.id ?? ''
  laborDlg.rows = selectedWO.value.map((w) => ({ woId: w.id, woNo: w.woNo, itemNo: w.itemNo, itemName: w.itemName, lineId: '', headcount: laborDlg.headcount }))
  autoAssign()
  laborDlg.open = true
}
async function confirmBatchLabor() {
  if (laborDlg.rows.some((r) => !r.lineId)) return ElMessage.warning('每張製令都要選線別（線別不夠時請減少勾選）')
  const lineIds = laborDlg.rows.map((r) => r.lineId)
  if (new Set(lineIds).size !== lineIds.length) return ElMessage.warning('同一條線只能上工一張製令，請調整線別')
  laborDlg.saving = true
  try {
    await api('/labor/batch-start', {
      method: 'POST',
      body: { items: laborDlg.rows.map((r) => ({ lineId: r.lineId, workOrderId: r.woId, headcount: r.headcount })) },
    })
    ElMessage.success(`已上工 ${laborDlg.rows.length} 條線`)
    laborDlg.open = false
    woTable.value?.clearSelection()
    await load()
  } catch (e) {
    fail(e)
  } finally {
    laborDlg.saving = false
  }
}
async function batchEnd() {
  const list = selectedRunning.value.flatMap((w) => laborOf(w.id).map((l) => `${l.lineCode}（${w.woNo}）`))
  try {
    await ElMessageBox.confirm(`下工以下線別：${list.join('、')}？`, '整批下工', { confirmButtonText: '確認下工', cancelButtonText: '返回', type: 'warning' })
    const r = await api<{ ended: number }>('/labor/batch-end', { method: 'POST', body: { workOrderIds: selectedRunning.value.map((w) => w.id) } })
    ElMessage.success(`已下工 ${r.ended} 條線`)
    woTable.value?.clearSelection()
    await load()
  } catch (e) {
    fail(e)
  }
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

// 批量轉製令：勾選的訂單一起轉，套用同一組管制選項；可選擇轉完立即開立
const convertDlg = reactive({ open: false, requireAuth: false, requireLabor: false, release: true })
const convertible = (o: SalesOrder) => o.status !== 'CANCELLED' && o.lines.some((l) => !l.woNo)
function openConvert() {
  if (!selectedSO.value.length) return ElMessage.info('請先勾選要轉製令的訂單')
  Object.assign(convertDlg, { open: true, requireAuth: false, requireLabor: false, release: session.can('wo:release') })
}
async function convert() {
  busy.value = true
  try {
    const created = await api<{ id: string; woNo: string }[]>('/sales-orders/convert', {
      method: 'POST',
      body: { salesOrderIds: selectedSO.value.map((o) => o.id), requireAuth: convertDlg.requireAuth, requireLabor: convertDlg.requireLabor },
    })
    convertDlg.open = false
    if (convertDlg.release) await release(created)
    else {
      ElMessage.success(`已建立 ${created.length} 張草稿製令：${created.map((w) => w.woNo).join('、')}`)
      await load()
    }
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
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
  woDlg.form = w
    ? { productId: p?.id ?? '', qty: w.plannedQty, dueDate: w.dueDate?.slice(0, 10) ?? null, requireAuth: w.requireAuth, requireLabor: w.requireLabor }
    : { productId: '', qty: 100, dueDate: null, requireAuth: false, requireLabor: false }
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
type StepState = 'wait' | 'run' | 'done'
// 全流程進度：第一站應到量＝計畫量，之後各站應到量＝上一站良品；
// 處理量（良品＋不良）達應到量且前站已完成才算完成；尚未到站或尚未開始回報為灰色
function flow(w: WorkOrder) {
  let arrived = w.plannedQty
  let prevDone = true
  return w.steps.map((s) => {
    const processed = s.good + s.defect
    let state: StepState = processed > 0 ? 'run' : 'wait'
    if (w.status === 'COMPLETED' || (prevDone && arrived > 0 && processed >= arrived)) state = 'done'
    const step = { ...s, arrived, state }
    prevDone = state === 'done'
    arrived = s.good
    return step
  })
}
// 發料狀態：只有出庫扣帳料件需要倉庫發料；到站扣帳料件於掃描時自動扣
function issueState(w: WorkOrder): { text: string; type: 'info' | 'danger' | 'warning' | 'success'; note?: string } {
  const { items, done, arrival } = w.issue
  const note = arrival ? `到站扣帳 ${arrival} 項免發` : undefined
  if (!items) return { text: '不需發料', type: 'info', note }
  if (w.status === 'DRAFT') return { text: `開立後發料（${items} 項）`, type: 'info', note }
  if (w.status === 'CANCELLED') return { text: '已取消', type: 'info' }
  if (done >= items) return { text: `已發完 ${done}/${items}`, type: 'success', note }
  if (!done) return { text: `待發料 0/${items}`, type: 'danger', note }
  return { text: `部分發料 ${done}/${items}`, type: 'warning', note }
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
.tools { display: flex; gap: 8px; }
.tools .el-button { margin-left: 0; }
.so-list { margin: 0 0 12px; padding-left: 20px; line-height: 1.8; }
.ctl-opts { display: flex; flex-direction: column; align-items: flex-start; }
.ops .el-button { margin-left: 0; }
.w-full { width: 100%; }
.flow { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 2px; }
.arrow { color: var(--text-muted); font-size: 12px; padding: 0 1px; }
.step { display: inline-flex; flex-direction: column; align-items: center; padding: 2px 8px; border-radius: 6px; font-size: 12px; line-height: 1.35; border: 1px solid transparent; }
.step small { font-size: 11px; opacity: .9; }
.step .bad { color: var(--status-critical); font-weight: 600; margin-left: 2px; }
.s-wait { background: var(--surface-2); color: var(--text-muted); border-color: var(--border); }
.s-run { background: var(--series-1-soft); color: var(--series-1); border-color: var(--series-1); }
.s-done { background: color-mix(in srgb, var(--status-good) 16%, transparent); color: var(--status-good); border-color: var(--status-good); }
.legend { font-weight: 400; font-size: 11px; color: var(--text-muted); margin-left: 8px; }
.dot { display: inline-block; width: 9px; height: 9px; border-radius: 2px; margin: 0 2px 0 6px; border-width: 1px; border-style: solid; vertical-align: -1px; }
.search { width: 220px; }
.station-filter { width: 150px; }
.running { color: var(--status-good); margin-top: 2px; }
.opt-r { float: right; color: var(--text-muted); font-size: 12px; margin-left: 12px; }
</style>
