<template>
  <div>
    <div class="bar">
      <span class="muted small">成品末站回報的良品先進「完工暫存倉」；勾選已完工的製令按入庫，待入庫數量轉進成品倉。</span>
      <el-checkbox v-model="showDone" size="small">顯示已入庫完的製令</el-checkbox>
      <el-button size="small" @click="load">重新整理</el-button>
    </div>
    <el-table ref="table" v-loading="loading" :data="shown" size="small" row-key="id" empty-text="沒有待入庫的製令"
      @selection-change="(v: Row[]) => (picked = v)">
      <el-table-column type="selection" width="44" :selectable="canPick" />
      <el-table-column label="製令" prop="woNo" width="190" />
      <el-table-column label="產品" min-width="180">
        <template #default="{ row }"><b>{{ row.itemNo }}</b> {{ row.itemName }}</template>
      </el-table-column>
      <el-table-column label="狀態" width="100">
        <template #default="{ row }">
          <el-tooltip :disabled="canPick(row) || row.pendingQty <= 0" content="未完工，不能入庫" placement="top">
            <el-tag size="small" :type="tagOf[row.status]">{{ statusText[row.status] }}</el-tag>
          </el-tooltip>
        </template>
      </el-table-column>
      <el-table-column label="計畫" width="80" align="right"><template #default="{ row }"><span class="num">{{ fmt.int(row.plannedQty) }}</span></template></el-table-column>
      <el-table-column label="完工良品" width="90" align="right"><template #default="{ row }"><span class="num">{{ fmt.int(row.goodQty) }}</span></template></el-table-column>
      <el-table-column label="已入庫" width="90" align="right"><template #default="{ row }"><span class="num">{{ fmt.int(row.storedQty) }}</span></template></el-table-column>
      <el-table-column label="待入庫" width="100" align="right">
        <template #default="{ row }"><b class="num" :class="{ muted: row.pendingQty <= 0 }">{{ fmt.int(row.pendingQty) }}</b> <span class="muted small">{{ row.unit }}</span></template>
      </el-table-column>
    </el-table>

    <div class="foot">
      <span v-if="picked.length" class="sum">
        已勾 {{ picked.length }} 張：<span v-for="t in totals" :key="t.itemNo" class="tot"><b>{{ t.itemNo }}</b> {{ t.itemName }} <b class="num">{{ fmt.int(t.qty) }}</b> {{ t.unit }}</span>
      </span>
      <span v-else class="muted small">可入庫 {{ pickable.length }} 張</span>
      <el-button size="small" :disabled="!pickable.length" @click="pickAll">全選可入庫</el-button>
      <el-button size="small" @click="router.push('/inventory')">查看庫存</el-button>
      <el-button v-if="canEdit" type="primary" :disabled="!picked.length" :loading="busy" @click="submit">入庫（{{ picked.length }}）</el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, type TableInstance } from 'element-plus'
import { api, fmt } from '../../api'
import { useSession } from '../../stores/session'
import { statusText } from '../dashboard/types'

// 完工入庫：已完工／結案的製令可批量勾選，把完工暫存倉的待入庫數量轉進成品倉；生產中只顯示不能勾
interface Row {
  id: string; woNo: string; itemNo: string; itemName: string; unit: string; status: string
  plannedQty: number; goodQty: number; storedQty: number; pendingQty: number
}
const emit = defineEmits<{ done: [] }>()
const router = useRouter()
const session = useSession()
const canEdit = computed(() => session.can('inventory:edit'))
const tagOf: Record<string, string> = { RELEASED: 'warning', IN_PROGRESS: 'primary', COMPLETED: 'success', CLOSED: 'success' }

const rows = ref<Row[]>([])
const picked = ref<Row[]>([])
const showDone = ref(false)
const loading = ref(false)
const busy = ref(false)
const table = ref<TableInstance>()

const canPick = (r: Row) => (r.status === 'COMPLETED' || r.status === 'CLOSED') && r.pendingQty > 0
const shown = computed(() => rows.value.filter((r) => showDone.value || r.pendingQty > 0 || !['COMPLETED', 'CLOSED'].includes(r.status)))
const pickable = computed(() => shown.value.filter(canPick))
const totals = computed(() => {
  const m = new Map<string, { itemNo: string; itemName: string; unit: string; qty: number }>()
  for (const r of picked.value) {
    const t = m.get(r.itemNo) ?? { itemNo: r.itemNo, itemName: r.itemName, unit: r.unit, qty: 0 }
    t.qty += r.pendingQty
    m.set(r.itemNo, t)
  }
  return [...m.values()]
})

async function load() {
  loading.value = true
  try {
    rows.value = await api<Row[]>('/putaway')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}
function pickAll() {
  table.value?.clearSelection()
  for (const r of pickable.value) table.value?.toggleRowSelection(r, true)
}
async function submit() {
  const list = picked.value
  try {
    await ElMessageBox.confirm(
      `${list.map((r) => `${r.woNo} ${fmt.int(r.pendingQty)}`).join('、')}。合計 ${totals.value.map((t) => `${t.itemNo} ${fmt.int(t.qty)} ${t.unit}`).join('、')}，從完工暫存倉轉入成品倉？`,
      `完工入庫 ${list.length} 張製令`, { confirmButtonText: '確認入庫', cancelButtonText: '返回', type: 'warning' })
  } catch {
    return
  }
  busy.value = true
  try {
    const res = await api<{ itemNo: string; qty: number; unit: string }[]>('/putaway', { method: 'POST', body: { workOrderIds: list.map((r) => r.id) } })
    ElMessage.success(`已入成品倉：${res.map((t) => `${t.itemNo} ${fmt.int(t.qty)} ${t.unit}`).join('、')}`)
    emit('done')
    await load()
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    busy.value = false
  }
}

onMounted(load)
defineExpose({ load })
</script>

<style scoped>
.bar { display: flex; align-items: center; gap: 12px; margin-bottom: 10px; flex-wrap: wrap; }
.bar > span { margin-right: auto; }
.foot { display: flex; align-items: center; gap: 8px; margin-top: 12px; flex-wrap: wrap; justify-content: flex-end; }
.foot .el-button { margin-left: 0; }
.sum { margin-right: auto; font-size: 13px; }
.tot { margin-right: 12px; }
.small { font-size: 12px; }
</style>
