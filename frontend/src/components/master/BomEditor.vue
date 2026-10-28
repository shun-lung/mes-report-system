<template>
  <div>
    <div class="bar">
      <span class="muted small">修改 BOM 只影響之後新開立的製令；已開立的製令使用開立當時的用料複本。</span>
      <el-button v-if="canEdit" type="primary" size="small" @click="edit()">新增料件</el-button>
    </div>
    <el-table :data="lines" size="small" empty-text="尚未設定 BOM">
      <el-table-column label="料件" min-width="170">
        <template #default="{ row }">
          <b>{{ row.itemNo }}</b> {{ row.itemName }}
          <div class="muted small">{{ categoryName[row.category] }}</div>
        </template>
      </el-table-column>
      <el-table-column label="類型" width="86">
        <template #default="{ row }">{{ materialTypeName[row.materialType] }}</template>
      </el-table-column>
      <el-table-column label="用量" width="110" align="right">
        <template #default="{ row }"><span class="num">{{ row.qtyPer }}</span> {{ row.unit }}<div v-if="row.scrapRate" class="muted small">損耗 {{ row.scrapRate }}%</div></template>
      </el-table-column>
      <el-table-column label="扣帳" min-width="130">
        <template #default="{ row }">
          {{ timingName[row.effectiveDeductTiming] }}
          <span v-if="row.consumeStationName" class="muted">· {{ row.consumeStationName }}</span>
          <div v-if="!row.deductTiming" class="muted small">依料件預設</div>
        </template>
      </el-table-column>
      <el-table-column label="替代料" min-width="150">
        <template #default="{ row }">
          <el-tag v-for="s in row.substitutes" :key="s.id" size="small" :closable="canEdit" class="sub"
            @close="removeSub(s.id)">{{ s.itemNo }} ×{{ s.ratio }}</el-tag>
          <el-button v-if="canEdit" size="small" text @click="openSub(row)">＋</el-button>
        </template>
      </el-table-column>
      <el-table-column v-if="canEdit" width="110">
        <template #default="{ row }">
          <el-button size="small" text @click="edit(row)">編輯</el-button>
          <el-popconfirm title="從 BOM 移除此料件？" @confirm="remove(row.id)">
            <template #reference><el-button size="small" text type="danger">刪除</el-button></template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dlg.open" :title="dlg.id ? '編輯 BOM 料件' : '新增 BOM 料件'" width="min(480px, 94vw)" append-to-body>
      <el-form label-width="96px" @submit.prevent>
        <el-form-item label="料件" required>
          <el-select v-model="dlg.form.childProductId" filterable :disabled="!!dlg.id" placeholder="搜尋品號或品名">
            <el-option v-for="p in candidates" :key="p.id" :label="`${p.itemNo} ${p.name}`" :value="p.id">
              <span>{{ p.itemNo }} {{ p.name }}</span><span class="opt-cat">{{ categoryName[p.category] }}</span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="類型">
          <el-radio-group v-model="dlg.form.materialType">
            <el-radio value="MAIN">基本材料</el-radio>
            <el-radio value="AUX">輔助料件</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="用量" required>
          <el-input-number v-model="dlg.form.qtyPer" :min="0.000001" :precision="6" :step="0.1" />
          <span class="unit">{{ childUnit }} ／每單位成品</span>
        </el-form-item>
        <el-form-item label="損耗率"><el-input-number v-model="dlg.form.scrapRate" :min="0" :max="100" :precision="2" /> <span class="unit">%</span></el-form-item>
        <el-form-item label="扣帳時機">
          <el-select v-model="dlg.form.deductTiming">
            <el-option label="依料件預設" value="" />
            <el-option label="到站扣帳" value="ON_ARRIVAL" />
            <el-option label="出庫扣帳" value="ON_ISSUE" />
          </el-select>
        </el-form-item>
        <el-form-item label="耗用站點">
          <el-select v-model="dlg.form.consumeStationId" clearable placeholder="到站扣帳時必填">
            <el-option v-for="s in stations" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlg.open = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">儲存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="sub.open" :title="`替代料：${sub.label}`" width="min(440px, 94vw)" append-to-body>
      <el-form label-width="80px" @submit.prevent>
        <el-form-item label="替代料" required>
          <el-select v-model="sub.form.substituteProductId" filterable placeholder="搜尋品號或品名">
            <el-option v-for="p in subCandidates" :key="p.id" :label="`${p.itemNo} ${p.name}`" :value="p.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="換算比">
          <el-input-number v-model="sub.form.ratio" :min="0.0001" :precision="4" :step="0.1" />
          <div class="hint">替代料用量 = 原用量 × 換算比，例如水性膠多用 10% 填 1.1</div>
        </el-form-item>
        <el-form-item label="優先序"><el-input-number v-model="sub.form.priority" :min="0" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="sub.open = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveSub">加入</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../../api'
import { categoryName, materialTypeName, timingName } from '../../labels'
import { useSession } from '../../stores/session'
import type { Product, Station } from './types'

interface Sub { id: string; productId: string; itemNo: string; itemName: string; ratio: number; priority: number }
interface Line {
  id: string; childProductId: string; itemNo: string; itemName: string; unit: string; category: string; materialType: string
  qtyPer: number; scrapRate: number; deductTiming: string | null; effectiveDeductTiming: string
  consumeStationId: string | null; consumeStationName: string | null; sortOrder: number; substitutes: Sub[]
}

const props = defineProps<{ productId: string; products: Product[]; stations: Station[] }>()
const emit = defineEmits<{ changed: [] }>()
const session = useSession()
const canEdit = computed(() => session.can('bom:edit'))
const lines = ref<Line[]>([])
const saving = ref(false)
const blank = () => ({ childProductId: '', materialType: 'MAIN', qtyPer: 1, scrapRate: 0, deductTiming: '', consumeStationId: null as string | null, sortOrder: 0 })
const dlg = reactive({ open: false, id: '', form: blank() })
const sub = reactive({ open: false, bomId: '', label: '', exclude: [] as string[], form: { substituteProductId: '', ratio: 1, priority: 1 } })

const candidates = computed(() =>
  props.products.filter((p) => p.id !== props.productId && p.isActive && (dlg.id || !lines.value.some((l) => l.childProductId === p.id))),
)
const subCandidates = computed(() => props.products.filter((p) => p.isActive && !sub.exclude.includes(p.id)))
const childUnit = computed(() => props.products.find((p) => p.id === dlg.form.childProductId)?.unit ?? '')

async function load() {
  lines.value = await api<Line[]>(`/products/${props.productId}/bom`)
}
function edit(l?: Line) {
  dlg.id = l?.id ?? ''
  dlg.form = l
    ? { childProductId: l.childProductId, materialType: l.materialType, qtyPer: l.qtyPer, scrapRate: l.scrapRate,
        deductTiming: l.deductTiming ?? '', consumeStationId: l.consumeStationId, sortOrder: l.sortOrder }
    : { ...blank(), sortOrder: lines.value.length }
  dlg.open = true
}
async function run(fn: () => Promise<unknown>, done: () => void, msg = '已儲存') {
  saving.value = true
  try {
    await fn()
    ElMessage.success(msg)
    done()
    await load()
    emit('changed')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    saving.value = false
  }
}
const save = () =>
  run(
    () => api(dlg.id ? `/bom/${dlg.id}` : '/bom', {
      method: dlg.id ? 'PUT' : 'POST',
      body: { ...dlg.form, parentProductId: props.productId, consumeStationId: dlg.form.consumeStationId || null },
    }),
    () => (dlg.open = false),
  )
const remove = (id: string) => run(() => api(`/bom/${id}`, { method: 'DELETE' }), () => {}, '已移除')
function openSub(l: Line) {
  sub.bomId = l.id
  sub.label = `${l.itemNo} ${l.itemName}`
  sub.exclude = [l.childProductId, props.productId, ...l.substitutes.map((s) => s.productId)]
  sub.form = { substituteProductId: '', ratio: 1, priority: l.substitutes.length + 1 }
  sub.open = true
}
const saveSub = () => run(() => api(`/bom/${sub.bomId}/substitutes`, { method: 'POST', body: sub.form }), () => (sub.open = false), '已加入替代料')
const removeSub = (id: string) => run(() => api(`/bom-substitutes/${id}`, { method: 'DELETE' }), () => {}, '已移除替代料')

onMounted(load)
</script>

<style scoped>
.bar { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 10px; flex-wrap: wrap; }
.small { font-size: 12px; }
.sub { margin: 0 4px 4px 0; }
.unit { margin-left: 8px; color: var(--text-muted); }
.hint { width: 100%; font-size: 12px; color: var(--text-muted); }
.opt-cat { float: right; color: var(--text-muted); font-size: 12px; margin-left: 12px; }
</style>
