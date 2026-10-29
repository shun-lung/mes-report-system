<template>
  <div class="page">
    <div class="page-title">
      <h2>回報紀錄</h2>
      <span class="muted">掃錯可撤銷：系統產生負數的撤銷紀錄，並還原進度、扣帳與入庫；原紀錄保留</span>
    </div>

    <div class="filters">
      <el-date-picker v-model="date" type="date" value-format="YYYY-MM-DD" :clearable="false" style="--el-date-editor-width: 140px"
        :disabled="!!cardNo" @change="load" />
      <el-select v-model="shiftId" clearable placeholder="全部班別" class="w-s" @change="load">
        <el-option v-for="s in shifts" :key="s.id" :label="s.name" :value="s.id" />
      </el-select>
      <el-select v-model="lineId" clearable filterable placeholder="全部線別" class="w-l" @change="load">
        <el-option-group v-for="g in stations" :key="g.id" :label="g.name">
          <el-option v-for="l in g.lines" :key="l.id" :label="l.name" :value="l.id" />
        </el-option-group>
      </el-select>
      <el-input v-model="cardNo" clearable placeholder="流程卡號（查全部日期）" class="w-c" @keydown.enter="load" @clear="load" />
      <el-button @click="load">查詢</el-button>
      <span class="muted small">{{ summary }}</span>
    </div>

    <div class="card">
      <el-table :data="scans" size="small" empty-text="沒有回報紀錄" :row-class-name="rowClass">
        <el-table-column label="時間" width="120">
          <template #default="{ row }">
            <span class="num">{{ fmt.dateTime(row.scannedAt, tz) }}</span>
            <div class="muted small">{{ row.shift }}</div>
          </template>
        </el-table-column>
        <el-table-column label="線別" width="96">
          <template #default="{ row }">{{ row.line }}<div class="muted small">{{ row.station }}</div></template>
        </el-table-column>
        <el-table-column label="流程卡" min-width="180">
          <template #default="{ row }">
            <span class="num">{{ row.cardNo }}</span>
            <div class="muted small">{{ row.itemNo }}</div>
          </template>
        </el-table-column>
        <el-table-column label="良品" width="80" align="right">
          <template #default="{ row }"><b class="num qty">{{ signed(row.good) }}</b></template>
        </el-table-column>
        <el-table-column label="不良" min-width="150">
          <template #default="{ row }">
            <span v-if="row.defect" class="num bad qty">{{ signed(row.defect) }}</span>
            <div v-if="row.defects" class="muted small">{{ row.defects }}</div>
          </template>
        </el-table-column>
        <el-table-column label="人員" prop="user" width="80" />
        <el-table-column label="狀態" min-width="170">
          <template #default="{ row }">
            <template v-if="row.reversalOf">
              <el-tag size="small" type="warning">撤銷紀錄</el-tag>
              <div class="muted small">{{ fmt.dateTime(row.createdAt, tz) }} 撤銷：{{ row.note }}</div>
            </template>
            <el-tag v-else-if="row.reversed" size="small" type="info">已撤銷</el-tag>
            <el-button v-else-if="canUndo" size="small" text type="danger" @click="undo(row)">撤銷</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, fmt } from '../api'
import { useSession } from '../stores/session'

interface Scan {
  id: string; scannedAt: string; createdAt: string; workDate: string; shift: string; line: string; station: string
  woNo: string; cardNo: string; itemNo: string; good: number; defect: number; defects: string | null; user: string
  note: string | null; reversalOf: string | null; reversed: boolean
}

const session = useSession()
const tz = computed(() => session.factory?.timezone ?? 'Asia/Taipei')
// 組長只能撤銷本班；生管（製令編輯權限）可撤銷任何時段，後端會再檢查
const canUndo = computed(() => session.can('scan:report') || session.can('wo:edit'))
const date = ref('')
const shiftId = ref('')
const lineId = ref('')
const cardNo = ref('')
const scans = ref<Scan[]>([])
const shifts = ref<{ id: string; name: string }[]>([])
const stations = ref<{ id: string; name: string; lines: { id: string; code: string; name: string }[] }[]>([])

const signed = (v: number) => fmt.int(v)
const net = computed(() => scans.value.reduce((a, s) => ({ good: a.good + s.good, defect: a.defect + s.defect }), { good: 0, defect: 0 }))
const summary = computed(() => `${scans.value.length} 筆 · 淨良品 ${fmt.int(net.value.good)} · 淨不良 ${fmt.int(net.value.defect)}`)
const rowClass = ({ row }: { row: Scan }) => (row.reversed ? 'is-reversed' : row.reversalOf ? 'is-reversal' : '')

async function load() {
  const q = new URLSearchParams()
  if (date.value && !cardNo.value) q.set('date', date.value)
  if (shiftId.value) q.set('shiftId', shiftId.value)
  if (lineId.value) q.set('lineId', lineId.value)
  if (cardNo.value.trim()) q.set('cardNo', cardNo.value.trim())
  const res = await api<{ workDate: string; scans: Scan[] }>('/scans?' + q)
  date.value ||= res.workDate
  scans.value = res.scans
}

async function undo(row: Scan) {
  try {
    const { value } = await ElMessageBox.prompt(
      `撤銷 ${row.cardNo} 在「${row.station}」的回報（良品 ${fmt.int(row.good)}、不良 ${fmt.int(row.defect)}）？系統會還原各站進度、到站扣帳與完工入庫。`,
      '撤銷回報',
      { confirmButtonText: '撤銷', cancelButtonText: '返回', type: 'warning', inputPlaceholder: '撤銷原因（必填）',
        inputValidator: (v) => (v && v.trim() ? true : '請填寫撤銷原因') },
    )
    await api(`/scans/${row.id}/reverse`, { method: 'POST', body: { reason: value.trim() } })
    ElMessage.success('已撤銷')
    await load()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error((e as Error).message)
  }
}

onMounted(async () => {
  ;[shifts.value, stations.value] = await Promise.all([
    api<{ id: string; name: string }[]>('/shifts'),
    session.can('station:view') ? api<any[]>('/stations') : Promise.resolve([]),
  ])
  await load()
})
</script>

<style scoped>
.filters { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.w-s { width: 110px; }
.w-l { width: 130px; }
.w-c { width: 240px; }
.small { font-size: 12px; }
.bad { color: var(--div-neg-ink); }
:deep(.is-reversed) .qty { text-decoration: line-through; color: var(--text-muted); }
:deep(.is-reversal) td { background: color-mix(in srgb, var(--status-warning) 8%, transparent); }
</style>
