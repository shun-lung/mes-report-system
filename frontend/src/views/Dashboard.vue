<template>
  <div class="page">
    <div class="page-title">
      <h2>生產 8HR 看板</h2>
      <span v-if="d" class="secondary">
        {{ d.factory.code }} {{ d.factory.name }} · {{ d.shift.name }}
        {{ fmt.time(d.shift.start, tz) }}–{{ fmt.time(d.shift.end, tz) }} · 生產日 {{ d.shift.workDate }}
      </span>
      <div class="spacer" />
      <el-button size="small" :disabled="!d" @click="go(d!.prev)">◀ 上一班</el-button>
      <el-date-picker v-model="date" type="date" value-format="YYYY-MM-DD" size="small" :clearable="false"
        style="--el-date-editor-width: 136px" @change="load" />
      <el-select v-model="shiftId" size="small" class="w-shift" @change="load">
        <el-option v-for="s in d?.shifts ?? []" :key="s.id" :label="s.name" :value="s.id" />
      </el-select>
      <el-button size="small" :disabled="!d || isCurrent || isFutureShift(d.next)" @click="go(d!.next)">下一班 ▶</el-button>
      <el-button size="small" :type="isCurrent ? '' : 'primary'" @click="resetToNow">目前班別</el-button>
      <span class="muted small">{{ isCurrent ? `每 60 秒更新 · ${updated}` : `歷史班別 · ${updated}` }}</span>
    </div>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />

    <el-alert v-if="d && isCurrent && shiftPct < 15" type="info" show-icon :closable="false"
      :title="`${d.shift.name}剛開始，資料仍少；可按「◀ 上一班」查看上一個完整班別。`" />

    <template v-if="d">
      <!-- 班別進度 -->
      <div class="progress card">
        <div class="pg-track">
          <div class="pg-fill" :style="{ width: shiftPct + '%' }" />
          <span v-for="(h, i) in d.hours" :key="h" class="pg-tick" :style="{ left: (i / d.hours.length) * 100 + '%' }">
            {{ fmt.time(h, tz).slice(0, 2) }}
          </span>
        </div>
        <span class="num pg-text">{{ elapsedText }}</span>
      </div>

      <!-- KPI -->
      <div class="kpis">
        <div class="kpi hero">
          <div class="k-label">成品完工入庫</div>
          <div class="k-value num">{{ fmt.int(k.fgOutput) }}<small> 雙</small></div>
          <div class="k-sub">
            應達 <span class="num">{{ fmt.int(k.fgTargetToDate) }}</span> ·
            <b :class="'ink-' + attainmentClass(fgAtt)">{{ attainmentGlyph(fgAtt) }} {{ fgAtt === null ? '—' : fmt.pct(fgAtt) }}</b>
          </div>
        </div>
        <div class="kpi">
          <div class="k-label">半成品入暫存倉</div>
          <div class="k-value num">{{ fmt.int(k.sfgOutput) }}<small> 雙</small></div>
          <div class="k-sub">鞋面 → 成型暫存倉</div>
        </div>
        <div class="kpi">
          <div class="k-label">全站良品產出</div>
          <div class="k-value num">{{ fmt.int(k.moves) }}<small> 雙</small></div>
          <div class="k-sub">{{ d.stations.length }} 站合計</div>
        </div>
        <div class="kpi">
          <div class="k-label">良率</div>
          <div class="k-value num">{{ fmt.pct(k.yield) }}</div>
          <div class="k-sub">不良 <span class="num">{{ fmt.int(k.defects) }}</span> 雙</div>
        </div>
        <div class="kpi">
          <div class="k-label">人時效率</div>
          <div class="k-value num">{{ fmt.pct(k.efficiency) }}</div>
          <div class="k-sub">標準 {{ fmt.dec(k.earnedHours) }} ／ 投入 {{ fmt.dec(k.manHours) }} 人時</div>
        </div>
        <div class="kpi">
          <div class="k-label">在製 WIP</div>
          <div class="k-value num">{{ fmt.int(k.wip) }}<small> 雙</small></div>
          <div class="k-sub">各站可報未報</div>
        </div>
        <div class="kpi">
          <div class="k-label">開線</div>
          <div class="k-value num">{{ k.runningLines }}<small> / {{ k.totalLines }}</small></div>
          <div class="k-sub">
            <span v-if="idleLines" class="warn">◐ 停滯 {{ idleLines }} 條</span>
            <span v-if="offLines">{{ idleLines ? ' · ' : '' }}○ 未開線 {{ offLines }} 條</span>
            <span v-if="!idleLines && !offLines">全部運轉</span>
          </div>
        </div>
        <router-link to="/alerts" class="kpi link">
          <div class="k-label">待處理缺料預警</div>
          <div class="k-value num">{{ k.pendingAlerts }}<small> 筆</small></div>
          <div class="k-sub">{{ k.pendingAlerts ? '⚠ 前往處理 →' : '無' }}</div>
        </router-link>
      </div>

      <div class="row-main">
        <div class="card grid-card">
          <h3>每小時產出 × 目標 <small>點站點列可收合線別；滑過格子看明細</small></h3>
          <HourlyGrid :hours="d.hours" :stations="d.stations" :now="d.now" :tz="tz" />
        </div>
        <div class="card">
          <h3>成品累計完工 <small>{{ finalStations.map((s) => s.name).join('、') }}站良品</small></h3>
          <CumulativeChart v-if="finalStations.length" :hours="d.hours" :cells="finalCells" :now="d.now" :tz="tz" />
          <el-empty v-else :image-size="48" description="尚無生效的成品途程" />
        </div>
      </div>

      <div class="row-3">
        <div class="card">
          <h3>在製量（各站待做） <small>雙</small></h3>
          <BarList :items="wipItems" unit="雙" empty="無在製" />
        </div>
        <div class="card">
          <h3>不良 Pareto <small>本班</small></h3>
          <BarList :items="defectItems" unit="雙" empty="本班無不良" />
        </div>
        <div class="card">
          <h3>最近掃描</h3>
          <div class="feed">
            <div v-for="r in d.recent" :key="r.at + r.cardNo" class="feed-row">
              <span class="num muted">{{ fmt.time(r.at, tz) }}</span>
              <span class="feed-line">{{ r.line }}</span>
              <span class="feed-card num">{{ r.cardNo.split('-').slice(-2).join('-') }}</span>
              <span class="num">+{{ fmt.int(r.good) }}</span>
              <span v-if="r.defect" class="num bad">✕{{ fmt.int(r.defect) }}</span>
            </div>
            <el-empty v-if="!d.recent.length" :image-size="48" description="尚無掃描" />
          </div>
        </div>
      </div>

      <div class="card">
        <h3>線別狀態</h3>
        <el-table :data="allLines" size="small" :default-sort="{ prop: 'code' }">
          <el-table-column label="線別" prop="name" width="130" sortable :sort-by="(r: any) => r.code" />
          <el-table-column label="站點" prop="stationName" width="90" />
          <el-table-column label="狀態" width="110">
            <template #default="{ row }">
              <span class="status" :class="row.status"><i />{{ lineStatus[row.status as keyof typeof lineStatus] }}</span>
            </template>
          </el-table-column>
          <el-table-column label="生產中製令" min-width="220">
            <template #default="{ row }">
              <template v-if="row.currentWo">{{ row.currentWo }}<div class="muted small">{{ row.currentItem }}</div></template>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="人數" prop="headcount" width="70" align="right" />
          <el-table-column label="良品 / 應達" width="130" align="right">
            <template #default="{ row }"><span class="num">{{ fmt.int(row.good) }} / {{ fmt.int(row.targetToDate) }}</span></template>
          </el-table-column>
          <el-table-column label="達成率" width="90" align="right" sortable :sort-method="(a: any, b: any) => ratioOf(a) - ratioOf(b)">
            <template #default="{ row }">
              <b class="num" :class="'ink-' + attainmentClass(row.targetToDate >= 10 ? row.good / row.targetToDate : null)">
                {{ row.targetToDate >= 10 ? fmt.pct(row.good / row.targetToDate) : '—' }}
              </b>
            </template>
          </el-table-column>
          <el-table-column label="人時效率" width="90" align="right">
            <template #default="{ row }"><span class="num">{{ row.manHours ? fmt.pct(row.earnedHours / row.manHours) : '—' }}</span></template>
          </el-table-column>
          <el-table-column label="最後掃描" width="110" align="right">
            <template #default="{ row }"><span class="num">{{ row.lastScanAt ? ago(row.lastScanAt) : '—' }}</span></template>
          </el-table-column>
        </el-table>
      </div>

      <div class="card">
        <h3>製令進度 <small>未結製令與本班完工；依交期排序</small></h3>
        <el-table :data="d.workOrders" size="small">
          <el-table-column label="製令" min-width="190">
            <template #default="{ row }">
              <b>{{ row.woNo }}</b>
              <div class="small">
                <el-tag size="small" :type="woTag(row.status)">{{ statusText[row.status] }}</el-tag>
                <el-tag v-if="row.source === 'SHORTAGE'" size="small" type="warning" class="ml">缺料衍生</el-tag>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="品項" min-width="170">
            <template #default="{ row }">{{ row.itemNo }}<div class="muted small">{{ row.itemName }}</div></template>
          </el-table-column>
          <el-table-column label="來源" min-width="190">
            <template #default="{ row }">
              <template v-if="row.soNo">{{ row.customer }}<div class="muted small">{{ row.soNo }}</div></template>
              <template v-else-if="row.parentWoNo">支援 {{ row.parentWoNo }}</template>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="交期" width="100">
            <template #default="{ row }"><span class="num">{{ fmt.date(row.dueDate) }}</span></template>
          </el-table-column>
          <el-table-column label="各站進度（良品 / 計畫）" min-width="280">
            <template #default="{ row }">
              <div v-for="s in row.steps" :key="s.seq" class="step">
                <span class="step-name">{{ s.station }}</span>
                <span class="step-track"><span class="step-fill" :style="{ width: Math.min(100, (s.good / row.plannedQty) * 100) + '%' }" /></span>
                <span class="num step-val">{{ fmt.int(s.good) }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="完工 / 計畫" width="130" align="right">
            <template #default="{ row }">
              <b class="num">{{ fmt.int(row.finishedQty) }}</b><span class="num muted"> / {{ fmt.int(row.plannedQty) }}</span>
              <div class="muted small num">{{ fmt.pct(row.finishedQty / row.plannedQty) }}</div>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </template>
    <el-skeleton v-else-if="!error" :rows="8" animated />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, fmt } from '../api'
import BarList from '../components/dashboard/BarList.vue'
import CumulativeChart from '../components/dashboard/CumulativeChart.vue'
import HourlyGrid from '../components/dashboard/HourlyGrid.vue'
import { attainmentClass, attainmentGlyph, statusText, type Line, type ShiftDashboard } from '../components/dashboard/types'

const d = ref<ShiftDashboard | null>(null)
const date = ref('')
const shiftId = ref('')
const error = ref('')
const updated = ref('')
const lineStatus = { RUNNING: '運轉中', IDLE: '停滯', OFF: '未開線' }
let timer: number | undefined

const tz = computed(() => d.value?.factory.timezone ?? 'Asia/Taipei')
const k = computed(() => d.value!.kpi)
// 完工站可能不只一個（不同成品途程的最後一站），逐小時加總
const finalStations = computed(() => d.value?.stations.filter((s) => s.isFinal) ?? [])
const finalCells = computed(() =>
  d.value!.hours.map((_, i) =>
    finalStations.value.reduce(
      (acc, s) => ({ good: acc.good + s.hourly[i].good, defect: acc.defect + s.hourly[i].defect, target: acc.target + s.hourly[i].target }),
      { good: 0, defect: 0, target: 0 },
    ),
  ),
)
const fgAtt = computed(() => (k.value.fgTargetToDate >= 10 ? k.value.fgOutput / k.value.fgTargetToDate : null))
const isCurrent = computed(() => {
  if (!d.value) return false
  const now = new Date(d.value.now).getTime()
  return now >= new Date(d.value.shift.start).getTime() && now < new Date(d.value.shift.end).getTime()
})
const shiftPct = computed(() => {
  if (!d.value) return 0
  const s = new Date(d.value.shift.start).getTime()
  const e = new Date(d.value.shift.end).getTime()
  return Math.max(0, Math.min(100, ((new Date(d.value.now).getTime() - s) / (e - s)) * 100))
})
const elapsedText = computed(() => {
  if (!d.value) return ''
  const total = d.value.hours.length
  const mins = Math.round((shiftPct.value / 100) * total * 60)
  return shiftPct.value >= 100 ? `本班已結束（${total} 小時）` : `已進行 ${Math.floor(mins / 60)} 時 ${mins % 60} 分 / ${total} 小時`
})
const allLines = computed(() =>
  (d.value?.stations ?? []).flatMap((s) => s.lines.map((l) => ({ ...l, stationName: s.name }))),
)
const idleLines = computed(() => allLines.value.filter((l) => l.status === 'IDLE').length)
const offLines = computed(() => allLines.value.filter((l) => l.status === 'OFF').length)
const wipItems = computed(() =>
  (d.value?.stations ?? []).map((s) => ({
    label: s.name,
    value: s.wip,
    tip: [
      { label: '在製（可報未報）', value: fmt.int(s.wip) + ' 雙' },
      { label: '本班良品', value: fmt.int(s.good) + ' 雙' },
    ],
  })),
)
const defectItems = computed(() =>
  (d.value?.defects ?? []).map((x) => ({
    label: `${x.code} ${x.name}`,
    sub: x.station,
    value: x.qty,
    tip: [
      { label: '站點', value: x.station },
      { label: '不良數', value: fmt.int(x.qty) + ' 雙' },
      { label: '占本班不良', value: k.value.defects ? fmt.pct(x.qty / k.value.defects) : '—' },
    ],
  })),
)

function ratioOf(l: Line) {
  return l.targetToDate ? l.good / l.targetToDate : -1
}
function ago(v: string) {
  const m = Math.round((new Date(d.value!.now).getTime() - new Date(v).getTime()) / 60000)
  return m < 1 ? '剛剛' : m < 60 ? `${m} 分鐘前` : fmt.time(v, tz.value)
}
function woTag(s: string) {
  return ({ COMPLETED: 'success', IN_PROGRESS: 'primary', DRAFT: 'info' } as Record<string, string>)[s] ?? ''
}

async function load() {
  try {
    const q = new URLSearchParams()
    if (date.value) q.set('date', date.value)
    if (shiftId.value) q.set('shiftId', shiftId.value)
    d.value = await api<ShiftDashboard>('/dashboard/shift?' + q)
    date.value = d.value.shift.workDate
    shiftId.value = d.value.shift.id
    updated.value = fmt.time(d.value.now, tz.value)
    error.value = ''
  } catch (e) {
    error.value = (e as Error).message
  }
}
function go(ref: { shiftId: string; date: string }) {
  date.value = ref.date
  shiftId.value = ref.shiftId
  load()
}
function isFutureShift(_ref: { shiftId: string; date: string }) {
  return !!d.value && new Date(d.value.shift.end).getTime() > new Date(d.value.now).getTime()
}
function resetToNow() {
  date.value = ''
  shiftId.value = ''
  load()
}

onMounted(() => {
  load()
  timer = window.setInterval(() => isCurrent.value && load(), 60_000)
})
onUnmounted(() => window.clearInterval(timer))
</script>

<style scoped>
.spacer { flex: 1; }
.small { font-size: 12px; }
.w-shift { width: 96px; }
.progress { display: flex; align-items: center; gap: 16px; padding: 12px 16px 22px; }
.pg-track { position: relative; flex: 1; height: 8px; background: var(--surface-2); border-radius: 4px; }
.pg-fill { height: 100%; background: var(--series-1); border-radius: 4px; }
.pg-tick { position: absolute; top: 12px; font-size: 11px; color: var(--text-muted); transform: translateX(-50%); }
.pg-tick:first-of-type { transform: none; }
.pg-text { color: var(--text-secondary); font-size: 12px; white-space: nowrap; }

.kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(138px, 1fr)); gap: 12px; }
.kpi {
  background: var(--surface-1); border: 1px solid var(--border); border-radius: 10px; padding: 14px 16px;
  color: inherit; text-decoration: none; min-width: 0;
}
.kpi.hero { border-color: var(--series-1); }
.kpi.link:hover { border-color: var(--series-1); }
.k-label { font-size: 12px; color: var(--text-secondary); }
.k-value { font-size: 26px; font-weight: 650; margin: 4px 0 2px; letter-spacing: -0.5px; }
.k-value small { font-size: 13px; font-weight: 400; color: var(--text-muted); }
.k-sub { font-size: 12px; color: var(--text-muted); }
.warn { color: var(--text-secondary); }
.ink-neg2, .ink-neg1 { color: var(--div-neg-ink); }
.ink-pos1, .ink-pos2 { color: var(--div-pos-ink); }
.ink-mid { color: var(--text-primary); }

.row-main { display: grid; grid-template-columns: minmax(0, 1.75fr) minmax(0, 1fr); gap: 16px; align-items: start; }
.row-3 { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; align-items: start; }
@media (max-width: 1200px) {
  .row-main { grid-template-columns: 1fr; }
  .row-3 { grid-template-columns: 1fr 1fr; }
}
@media (max-width: 760px) {
  .row-3 { grid-template-columns: 1fr; }
  .k-value { font-size: 24px; }
}

.feed { display: flex; flex-direction: column; gap: 4px; font-size: 13px; max-height: 260px; overflow-y: auto; }
.feed-row { display: grid; grid-template-columns: 44px 64px 1fr auto auto; gap: 8px; align-items: center; padding: 3px 0; border-bottom: 1px solid var(--grid); }
.feed-line { font-weight: 500; }
.feed-card { color: var(--text-secondary); font-size: 12px; }
.bad { color: var(--div-neg-ink); }

.status { display: inline-flex; align-items: center; gap: 6px; }
.status i { width: 8px; height: 8px; border-radius: 50%; }
.status.RUNNING i { background: var(--status-good); }
.status.IDLE i { background: var(--status-warning); }
.status.OFF i { background: var(--status-off); }
.ml { margin-left: 4px; }

.step { display: grid; grid-template-columns: 56px 1fr 44px; gap: 8px; align-items: center; font-size: 12px; line-height: 1.9; }
.step-name { color: var(--text-secondary); }
.step-track { height: 8px; background: var(--surface-2); border-radius: 4px; overflow: hidden; }
.step-fill { display: block; height: 100%; background: var(--series-1); border-radius: 0 4px 4px 0; }
.step-val { text-align: right; }
</style>
