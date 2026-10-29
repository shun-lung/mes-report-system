<template>
  <el-dialog :model-value="modelValue" width="min(1080px, 98vw)" destroy-on-close
    :title="wo ? `${canIssue ? '發料' : '用料'} — ${wo.woNo}　${wo.itemNo} ${wo.itemName}　計畫 ${fmt.int(wo.plannedQty)} 雙` : ''"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)" @open="load">
    <el-table v-loading="loading" :data="rows" size="small" empty-text="此製令沒有用料">
      <el-table-column label="料件" min-width="190">
        <template #default="{ row }">
          <b>{{ row.itemNo }}</b> {{ row.itemName }}
          <div v-if="row.substitutedFrom" class="muted small">換料自 {{ row.substitutedFrom }}</div>
        </template>
      </el-table-column>
      <el-table-column label="扣帳方式" width="150">
        <template #default="{ row }">
          <template v-if="row.deductTiming === 'ON_ISSUE'">
            <el-tag size="small" type="warning">倉庫發料</el-tag>
            <div v-if="row.station" class="muted small">用於 {{ row.station }}站</div>
          </template>
          <template v-else>
            <el-tag size="small" type="info">到站扣帳</el-tag>
            <div class="muted small">{{ row.station ?? '' }} 報工時自動扣</div>
          </template>
        </template>
      </el-table-column>
      <el-table-column label="單位用量" width="86" align="right">
        <template #default="{ row }"><span class="num">{{ dec(row.qtyPer) }}</span></template>
      </el-table-column>
      <el-table-column label="應發" width="90" align="right">
        <template #default="{ row }"><span class="num">{{ dec(row.required) }}</span></template>
      </el-table-column>
      <el-table-column label="已發／已扣" width="96" align="right">
        <template #default="{ row }"><span class="num">{{ dec(row.issued) }}</span></template>
      </el-table-column>
      <el-table-column label="未發" width="90" align="right">
        <template #default="{ row }"><b class="num">{{ dec(left(row)) }}</b> <span class="muted small">{{ row.unit }}</span></template>
      </el-table-column>
      <el-table-column label="庫存" width="120" align="right">
        <template #default="{ row }">
          <span class="num" :class="{ short: row.stock < left(row) }">{{ dec(row.stock) }}</span>
          <div class="muted small">{{ row.warehouse ?? '未設定倉庫' }}</div>
        </template>
      </el-table-column>
      <el-table-column :label="canIssue ? '本次發料' : '狀態'" width="170">
        <template #default="{ row }">
          <el-tag v-if="left(row) <= 0" size="small" type="success">{{ row.deductTiming === 'ON_ISSUE' ? '已發完' : '已扣完' }}</el-tag>
          <span v-else-if="row.deductTiming !== 'ON_ISSUE'" class="muted small">不需倉庫發料</span>
          <el-input-number v-else-if="canIssue" v-model="qty[row.id]" size="small" :min="0" :max="Math.min(left(row), Math.max(row.stock, 0))"
            :precision="left(row) % 1 || row.stock % 1 ? 2 : 0" :step="1" class="qty" />
          <el-tag v-else size="small" :type="row.issued > 0 ? 'warning' : 'danger'">{{ row.issued > 0 ? '部分發料' : '待發料' }}</el-tag>
        </template>
      </el-table-column>
    </el-table>
    <p v-if="shortRows.length" class="warn small">
      庫存不足：{{ shortRows.map((r) => `${r.itemNo} 缺 ${dec(left(r) - Math.max(r.stock, 0))} ${r.unit}`).join('、') }}。可先發現有庫存，其餘到「缺料預警」採購。
    </p>
    <template #footer>
      <span v-if="canIssue" class="sum muted">本次發料 {{ picked.length }} 項</span>
      <el-button @click="emit('update:modelValue', false)">關閉</el-button>
      <el-button v-if="canIssue" type="warning" :disabled="!picked.length" :loading="busy" @click="submit">確認發料</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, fmt } from '../../api'
import { useSession } from '../../stores/session'
import type { WorkOrder } from '../dashboard/types'

// 發料：先列出製令用料（應發、已發、庫存），倉管確認各項本次發料量後才扣帳；到站扣帳料件只顯示
interface Material {
  id: string; itemNo: string; itemName: string; unit: string; materialType: string; qtyPer: number
  required: number; issued: number; deductTiming: 'ON_ISSUE' | 'ON_ARRIVAL'; station: string | null
  warehouse: string | null; stock: number; substitutedFrom: string | null
}
const props = defineProps<{ modelValue: boolean; wo: WorkOrder | null }>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; issued: [] }>()

const session = useSession()
const canIssue = computed(() => session.can('inventory:edit') && ['RELEASED', 'IN_PROGRESS'].includes(props.wo?.status ?? ''))
const rows = ref<Material[]>([])
const qty = reactive<Record<string, number>>({})
const loading = ref(false)
const busy = ref(false)

const left = (m: Material) => Math.max(0, Math.round((m.required - m.issued) * 10000) / 10000)
const dec = (v: number) => fmt.dec(v, v % 1 ? 2 : 0)
const shortRows = computed(() => rows.value.filter((m) => m.deductTiming === 'ON_ISSUE' && m.stock < left(m)))
const picked = computed(() => rows.value.filter((m) => m.deductTiming === 'ON_ISSUE' && (qty[m.id] ?? 0) > 0))

async function load() {
  if (!props.wo) return
  loading.value = true
  try {
    rows.value = await api<Material[]>(`/work-orders/${props.wo.id}/materials`)
    for (const k of Object.keys(qty)) delete qty[k]
    // 預設本次發料＝未發量，庫存不足時以現有庫存為上限
    for (const m of rows.value) if (m.deductTiming === 'ON_ISSUE') qty[m.id] = Math.min(left(m), Math.max(m.stock, 0))
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}
async function submit() {
  const list = picked.value
  try {
    await ElMessageBox.confirm(
      `發料 ${list.map((m) => `${m.itemNo} ${dec(qty[m.id])} ${m.unit}`).join('、')}？將自倉庫扣帳。`,
      '確認發料', { confirmButtonText: '確認發料', cancelButtonText: '返回', type: 'warning' })
  } catch {
    return
  }
  busy.value = true
  try {
    await api(`/work-orders/${props.wo!.id}/issue`, { method: 'POST', body: { lines: list.map((m) => ({ materialId: m.id, qty: qty[m.id] })) } })
    ElMessage.success(`已發料 ${list.length} 項：${list.map((m) => `${m.itemNo} ${dec(qty[m.id])}`).join('、')}`)
    emit('issued')
    emit('update:modelValue', false)
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.small { font-size: 12px; }
.qty { width: 130px; }
.short { color: var(--status-critical); font-weight: 600; }
.warn { color: var(--div-neg-ink); margin: 10px 0 0; }
.sum { margin-right: 12px; font-size: 13px; }
</style>
