<template>
  <div class="page">
    <div class="page-title"><h2>採購</h2><span class="muted">材料缺料時系統會自動加入採購單草稿，確認數量與供應商後下單</span></div>
    <div class="card">
      <el-tabs v-model="tab">
        <el-tab-pane label="採購單" name="po">
          <div class="bar">
            <el-radio-group v-model="filter" size="small" @change="load">
              <el-radio-button value="open">未結案</el-radio-button>
              <el-radio-button value="all">全部</el-radio-button>
            </el-radio-group>
            <el-button v-if="canEdit" type="primary" @click="openPO(null)">新增採購單</el-button>
          </div>
          <el-table :data="pos" size="small" empty-text="沒有採購單">
            <el-table-column label="採購單" min-width="190">
              <template #default="{ row }">
                <b>{{ row.poNo }}</b>
                <div class="small">
                  <el-tag size="small" :type="poTag[row.status]">{{ poStatus[row.status] }}</el-tag>
                  <el-tag v-if="row.source === 'SHORTAGE'" size="small" type="warning" class="ml">缺料自動</el-tag>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="供應商" min-width="110">
              <template #default="{ row }"><span v-if="row.supplier">{{ row.supplier }}</span><span v-else class="bad">未指定</span></template>
            </el-table-column>
            <el-table-column label="預計到貨" width="100"><template #default="{ row }"><span class="num">{{ fmt.date(row.expectedDate) }}</span></template></el-table-column>
            <el-table-column label="明細（訂購／已到）" min-width="300">
              <template #default="{ row }">
                <div v-for="l in row.lines" :key="l.id" class="line">
                  <span>{{ l.itemNo }} {{ l.itemName }}</span>
                  <span class="num">{{ q(l.qty) }}／<b :class="{ done: l.receivedQty >= l.qty }">{{ q(l.receivedQty) }}</b> {{ l.unit }}</span>
                  <span v-if="l.woNo" class="muted small">{{ l.woNo }}</span>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="200">
              <template #default="{ row }">
                <div class="ops">
                  <el-button v-if="row.status === 'DRAFT' && canEdit" size="small" @click="openPO(row)">編輯</el-button>
                  <el-button v-if="row.status === 'DRAFT' && canEdit" size="small" type="primary" @click="order(row)">下單</el-button>
                  <el-button v-if="['ORDERED', 'PARTIAL'].includes(row.status) && canReceive" size="small" type="primary" @click="openReceive(row)">收料</el-button>
                  <el-button v-if="['DRAFT', 'ORDERED'].includes(row.status) && canEdit && !row.lines.some((l: any) => l.receivedQty > 0)"
                    size="small" text type="danger" @click="cancel(row)">取消</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="供應商" name="suppliers">
          <div class="bar"><span /><el-button v-if="canEdit" type="primary" @click="openSupplier(null)">新增供應商</el-button></div>
          <el-table :data="suppliers" size="small" empty-text="尚無供應商">
            <el-table-column label="代碼" prop="code" width="90" />
            <el-table-column label="名稱" prop="name" min-width="140" />
            <el-table-column label="聯絡人" prop="contact" width="100" />
            <el-table-column label="電話" prop="phone" width="130" />
            <el-table-column label="交期（天）" prop="leadDays" width="96" align="right" />
            <el-table-column label="預設料件" prop="products" width="90" align="right" />
            <el-table-column label="狀態" width="80">
              <template #default="{ row }"><el-tag size="small" :type="row.isActive ? 'success' : 'info'">{{ row.isActive ? '往來中' : '停用' }}</el-tag></template>
            </el-table-column>
            <el-table-column v-if="canEdit" width="70"><template #default="{ row }"><el-button size="small" text @click="openSupplier(row)">編輯</el-button></template></el-table-column>
          </el-table>
          <p class="muted small">在「產品資料」可為每個材料設定預設供應商，缺料自動請購會依此分單。</p>
        </el-tab-pane>
      </el-tabs>
    </div>

    <!-- 採購單編輯 -->
    <el-dialog v-model="poDlg.open" :title="poDlg.id ? `編輯採購單 ${poDlg.no}` : '新增採購單'" width="min(720px, 96vw)">
      <el-form label-width="84px" @submit.prevent>
        <el-form-item label="供應商">
          <el-select v-model="poDlg.form.supplierId" clearable filterable placeholder="下單前必須指定" class="w-sup">
            <el-option v-for="s in suppliers.filter((x) => x.isActive)" :key="s.id" :label="`${s.code} ${s.name}`" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="預計到貨"><el-date-picker v-model="poDlg.form.expectedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item label="備註"><el-input v-model="poDlg.form.note" maxlength="500" /></el-form-item>
      </el-form>
      <el-table :data="poDlg.form.lines" size="small" empty-text="請加入料件">
        <el-table-column label="料件" min-width="220">
          <template #default="{ row }">
            <el-select v-model="row.productId" filterable placeholder="搜尋品號或品名" class="w-full">
              <el-option v-for="p in products" :key="p.id" :label="`${p.itemNo} ${p.name}`" :value="p.id" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="數量" width="150">
          <template #default="{ row }"><el-input-number v-model="row.qty" :min="0.0001" controls-position="right" class="w-full" /></template>
        </el-table-column>
        <el-table-column label="備註" min-width="140"><template #default="{ row }"><el-input v-model="row.note" maxlength="200" /></template></el-table-column>
        <el-table-column width="48">
          <template #default="{ $index }"><el-button size="small" text type="danger" aria-label="移除" @click="poDlg.form.lines.splice($index, 1)">✕</el-button></template>
        </el-table-column>
      </el-table>
      <el-button size="small" class="mt" @click="poDlg.form.lines.push({ productId: '', qty: 1, note: '', workOrderId: null })">＋ 加料件</el-button>
      <template #footer>
        <el-button @click="poDlg.open = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="savePO">儲存草稿</el-button>
      </template>
    </el-dialog>

    <!-- 收料 -->
    <el-dialog v-model="rcv.open" :title="`採購收料：${rcv.po?.poNo}`" width="min(640px, 96vw)">
      <el-form label-width="70px" @submit.prevent>
        <el-form-item label="入庫倉">
          <el-select v-model="rcv.warehouseId" class="w-sup">
            <el-option v-for="w in warehouses" :key="w.id" :label="`${w.code} ${w.name}`" :value="w.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="說明"><el-input v-model="rcv.note" maxlength="200" placeholder="例如：送貨單號" /></el-form-item>
      </el-form>
      <el-table :data="rcv.lines" size="small">
        <el-table-column label="料件" min-width="160"><template #default="{ row }">{{ row.itemNo }} {{ row.itemName }}</template></el-table-column>
        <el-table-column label="未到" width="90" align="right"><template #default="{ row }"><span class="num">{{ q(row.remaining) }}</span> {{ row.unit }}</template></el-table-column>
        <el-table-column label="本次收料" width="160">
          <template #default="{ row }"><el-input-number v-model="row.qty" :min="0" :max="row.remaining" controls-position="right" class="w-full" /></template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="rcv.open = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="receive">確認收料</el-button>
      </template>
    </el-dialog>

    <!-- 供應商 -->
    <el-dialog v-model="supDlg.open" :title="supDlg.id ? '編輯供應商' : '新增供應商'" width="min(440px, 94vw)">
      <el-form label-width="80px" @submit.prevent>
        <el-form-item label="代碼" required><el-input v-model="supDlg.form.code" maxlength="20" /></el-form-item>
        <el-form-item label="名稱" required><el-input v-model="supDlg.form.name" maxlength="100" /></el-form-item>
        <el-form-item label="聯絡人"><el-input v-model="supDlg.form.contact" maxlength="50" /></el-form-item>
        <el-form-item label="電話"><el-input v-model="supDlg.form.phone" maxlength="30" /></el-form-item>
        <el-form-item label="交期（天）"><el-input-number v-model="supDlg.form.leadDays" :min="0" :max="365" /></el-form-item>
        <el-form-item label="往來中"><el-switch v-model="supDlg.form.isActive" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="supDlg.open = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="saveSupplier">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, fmt } from '../api'
import { useSession } from '../stores/session'

interface Line { id: string; lineNo: number; productId: string; itemNo: string; itemName: string; unit: string; qty: number; receivedQty: number; workOrderId: string | null; woNo: string | null; note: string | null }
interface PO { id: string; poNo: string; status: string; source: string; supplierId: string | null; supplier: string | null; expectedDate: string | null; note: string | null; lines: Line[] }
interface Supplier { id: string; code: string; name: string; contact: string | null; phone: string | null; leadDays: number; isActive: boolean; products: number }

const session = useSession()
const canEdit = computed(() => session.can('purchase:edit'))
const canReceive = computed(() => session.can('inventory:edit'))
const tab = ref('po')
const filter = ref<'open' | 'all'>('open')
const busy = ref(false)
const pos = ref<PO[]>([])
const suppliers = ref<Supplier[]>([])
const products = ref<{ id: string; itemNo: string; name: string }[]>([])
const warehouses = ref<{ id: string; code: string; name: string; type: string; isActive: boolean }[]>([])
const poStatus: Record<string, string> = { DRAFT: '草稿', ORDERED: '已下單', PARTIAL: '部分到貨', RECEIVED: '全部到貨', CANCELLED: '已取消' }
const poTag: Record<string, string> = { DRAFT: 'info', ORDERED: 'primary', PARTIAL: 'warning', RECEIVED: 'success', CANCELLED: 'info' }
const q = (v: number) => fmt.dec(v, v % 1 === 0 ? 0 : 2)

const poDlg = reactive({
  open: false, id: '', no: '',
  form: { supplierId: null as string | null, expectedDate: null as string | null, note: '', lines: [] as { productId: string; qty: number; note: string; workOrderId: string | null }[] },
})
const rcv = reactive({ open: false, po: null as PO | null, warehouseId: '', note: '', lines: [] as { id: string; itemNo: string; itemName: string; unit: string; remaining: number; qty: number }[] })
const supDlg = reactive({ open: false, id: '', form: { code: '', name: '', contact: '', phone: '', leadDays: 7, isActive: true } })

async function load() {
  const status = filter.value === 'open' ? 'DRAFT,ORDERED,PARTIAL' : ''
  ;[pos.value, suppliers.value] = await Promise.all([
    api<PO[]>(`/purchase-orders?status=${status}`),
    api<Supplier[]>('/suppliers').catch(() => []),
  ])
}
function fail(e: unknown) {
  if (e !== 'cancel' && e !== 'close') ElMessage.error((e as Error).message)
}

function openPO(p: PO | null) {
  poDlg.id = p?.id ?? ''
  poDlg.no = p?.poNo ?? ''
  poDlg.form = p
    ? { supplierId: p.supplierId, expectedDate: p.expectedDate?.slice(0, 10) ?? null, note: p.note ?? '',
        lines: p.lines.map((l) => ({ productId: l.productId, qty: l.qty, note: l.note ?? '', workOrderId: l.workOrderId })) }
    : { supplierId: null, expectedDate: null, note: '', lines: [{ productId: '', qty: 1, note: '', workOrderId: null }] }
  poDlg.open = true
}
async function savePO() {
  const lines = poDlg.form.lines.filter((l) => l.productId && l.qty > 0)
  if (!lines.length) return ElMessage.error('請至少加入一個料件')
  busy.value = true
  try {
    const body = { ...poDlg.form, expectedDate: poDlg.form.expectedDate ?? '', lines }
    if (poDlg.id) await api(`/purchase-orders/${poDlg.id}`, { method: 'PUT', body })
    else {
      const r = await api<{ poNo: string }>('/purchase-orders', { method: 'POST', body })
      ElMessage.success(`已建立採購單草稿 ${r.poNo}`)
    }
    poDlg.open = false
    await load()
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}
async function order(p: PO) {
  try {
    await ElMessageBox.confirm(`採購單 ${p.poNo} 向「${p.supplier ?? '（未指定）'}」下單？下單後不可再修改明細。`, '下單', { confirmButtonText: '下單', cancelButtonText: '返回' })
    await api(`/purchase-orders/${p.id}/order`, { method: 'POST' })
    ElMessage.success('已下單')
    await load()
  } catch (e) {
    fail(e)
  }
}
async function cancel(p: PO) {
  try {
    await ElMessageBox.confirm(`取消採購單 ${p.poNo}？${p.source === 'SHORTAGE' ? '相關缺料預警會回到待處理。' : ''}`, '取消採購單', {
      confirmButtonText: '取消採購單', cancelButtonText: '返回', type: 'warning',
    })
    await api(`/purchase-orders/${p.id}/cancel`, { method: 'POST' })
    ElMessage.success('已取消')
    window.dispatchEvent(new Event('mes:alerts-changed'))
    await load()
  } catch (e) {
    fail(e)
  }
}
async function openReceive(p: PO) {
  if (!warehouses.value.length) warehouses.value = (await api<any[]>('/warehouses')).filter((w) => w.isActive)
  rcv.po = p
  rcv.warehouseId = warehouses.value.find((w) => w.type === 'RAW')?.id ?? warehouses.value[0]?.id ?? ''
  rcv.note = ''
  rcv.lines = p.lines.filter((l) => l.qty > l.receivedQty)
    .map((l) => ({ id: l.id, itemNo: l.itemNo, itemName: l.itemName, unit: l.unit, remaining: l.qty - l.receivedQty, qty: l.qty - l.receivedQty }))
  rcv.open = true
}
async function receive() {
  const lines = rcv.lines.filter((l) => l.qty > 0).map((l) => ({ lineId: l.id, qty: l.qty }))
  if (!lines.length) return ElMessage.error('請輸入收料數量')
  busy.value = true
  try {
    await api(`/purchase-orders/${rcv.po!.id}/receive`, { method: 'POST', body: { warehouseId: rcv.warehouseId, note: rcv.note, lines } })
    ElMessage.success('已收料入庫')
    rcv.open = false
    await load()
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}
function openSupplier(s: Supplier | null) {
  supDlg.id = s?.id ?? ''
  supDlg.form = s
    ? { code: s.code, name: s.name, contact: s.contact ?? '', phone: s.phone ?? '', leadDays: s.leadDays, isActive: s.isActive }
    : { code: '', name: '', contact: '', phone: '', leadDays: 7, isActive: true }
  supDlg.open = true
}
async function saveSupplier() {
  busy.value = true
  try {
    await api(supDlg.id ? `/suppliers/${supDlg.id}` : '/suppliers', { method: supDlg.id ? 'PUT' : 'POST', body: supDlg.form })
    ElMessage.success('已儲存')
    supDlg.open = false
    await load()
  } catch (e) {
    fail(e)
  } finally {
    busy.value = false
  }
}

onMounted(async () => {
  await load()
  if (canEdit.value && session.can('product:view')) products.value = (await api<any[]>('/products')).filter((p) => p.isActive)
})
</script>

<style scoped>
.bar { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 10px; flex-wrap: wrap; }
.small { font-size: 12px; }
.ml { margin-left: 4px; }
.bad { color: var(--div-neg-ink); }
.line { display: flex; flex-wrap: wrap; gap: 4px 10px; line-height: 1.8; }
.done { color: var(--status-good); }
.ops { display: flex; flex-wrap: wrap; gap: 4px; }
.ops .el-button { margin-left: 0; }
.w-sup { width: min(300px, 100%); }
.w-full { width: 100%; }
.mt { margin-top: 8px; }
</style>
