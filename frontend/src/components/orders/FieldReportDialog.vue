<template>
  <el-dialog :model-value="modelValue" width="min(1180px, 98vw)" top="3vh" destroy-on-close class="field-dialog"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)" @open="init" @closed="emit('closed')">
    <template #header>
      <div class="head">
        <b>現場報數（{{ workOrders.length }} 張製令）</b>
        <el-select v-model="stationId" size="default" class="station" placeholder="站點" @change="onStation">
          <el-option v-for="s in stations" :key="s.id" :label="s.name" :value="s.id" />
        </el-select>
        <span class="muted small">① 選線別、人數 → 上工　② 點流程卡報數量（本站報完自動下工）</span>
      </div>
    </template>

    <el-alert v-if="banner" :type="banner.type" :title="banner.text" show-icon :closable="false" class="banner" />

    <el-table :data="rows" size="small" row-key="wo.id" empty-text="沒有勾選製令" v-loading="loading">
      <el-table-column label="製令" min-width="190">
        <template #default="{ row }">
          <b>{{ row.wo.woNo }}</b>
          <el-tag v-if="row.wo.requireLabor" size="small" type="danger" effect="plain" class="ml">強制上工</el-tag>
          <div class="muted small">{{ row.wo.itemNo }} {{ row.wo.itemName }}</div>
        </template>
      </el-table-column>
      <el-table-column label="線別 · 人數" min-width="250">
        <template #default="{ row }">
          <div v-if="row.run" class="running">
            ● {{ row.run.lineCode }} 上工中 · {{ row.run.headcount }} 人 · {{ time(row.run.startedAt) }} 起
          </div>
          <div v-else-if="canLabor" class="assign">
            <el-select v-model="row.lineId" size="small" :placeholder="allBusy ? '線別全忙，選了會換線' : '選線別'" class="line" @change="(id: string) => onLine(row, id)">
              <el-option v-for="l in stationLines" :key="l.id" :value="l.id" :label="l.code + (busyLabel(l.id, row) ? `（${busyLabel(l.id, row)}）` : '')" />
            </el-select>
            <el-input-number v-model="row.headcount" :min="1" :max="200" size="small" class="head-n" />
            <span class="muted small">人</span>
          </div>
          <span v-else class="muted">未上工</span>
        </template>
      </el-table-column>
      <el-table-column label="本站流程卡（點選報數量）" min-width="360">
        <template #default="{ row }">
          <div class="cards">
            <el-button v-for="c in cardsOf(row.wo.id)" :key="c.cardNo" size="small" :disabled="!canScan"
              :type="active?.cardNo === c.cardNo ? 'primary' : 'default'" @click="pick(row, c)">
              <span class="num">{{ shortCard(c.cardNo) }}</span><small>&nbsp;可報 {{ fmt.int(c.remaining) }}</small>
            </el-button>
            <el-popconfirm v-if="canScan && cardsOf(row.wo.id).length > 1" :title="`${cardsOf(row.wo.id).length} 張卡全部以良品報完？`"
              confirm-button-text="全部報完" cancel-button-text="返回" width="220" @confirm="reportAll(row)">
              <template #reference><el-button size="small" type="success" plain>全部良品（{{ fmt.int(sumOf(row.wo.id)) }}）</el-button></template>
            </el-popconfirm>
            <span v-if="!cardsOf(row.wo.id).length" class="muted small">本站沒有可報的卡（上站未完成或本站已報完）</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column v-if="canLabor" width="76">
        <template #default="{ row }">
          <el-button v-if="row.run" size="small" @click="endRows([row])">下工</el-button>
        </template>
      </el-table-column>
    </el-table>

    <section v-if="active" class="report">
      <div class="report-title">
        回報 <b class="num">{{ active.cardNo }}</b>
        <span class="muted">· {{ lineCode(active.lineId) }}{{ active.row.run ? '' : '（未上工，不計工時）' }}</span>
      </div>
      <CardReport :card-no="active.cardNo" :remaining="active.remaining" :line-id="active.lineId" :defect-codes="defectCodes"
        :device-id="`ORDERS-${lineCode(active.lineId)}`" size="default" confirm-text="確認回報" @done="onReported" @error="(m) => show('error', m)"
        @cancel="active = null" />
    </section>

    <template #footer>
      <el-button @click="emit('update:modelValue', false)">關閉</el-button>
      <el-button v-if="canLabor" :disabled="!running.length" @click="endRows(running)">下工（{{ running.length }}）</el-button>
      <el-button v-if="canLabor" type="success" :disabled="!pending.length" :loading="busy" @click="startRows">上工（{{ pending.length }}）</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessageBox } from 'element-plus'
import { api, fmt } from '../../api'
import { useSession } from '../../stores/session'
import type { WorkOrder } from '../dashboard/types'
import CardReport, { type DefectCode, type ScanResult } from '../station/CardReport.vue'

// 現場報數：勾選的製令在同一畫面完成「上工（人數＋工時起算）→ 報數量 → 下工」；本站報完時後端自動下工
const props = defineProps<{ modelValue: boolean; workOrders: WorkOrder[]; station?: string }>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; closed: [] }>()

interface StationLine { id: string; code: string; stationId: string; stationName: string; stdHeadcount: number }
interface OpenLabor { lineId: string; lineCode: string; workOrderId: string; woNo: string; headcount: number; startedAt: string }
interface StationCard { workOrderId: string; cardNo: string; qty: number; remaining: number }
interface Row { wo: WorkOrder; lineId: string; headcount: number; run: OpenLabor | null }

const session = useSession()
const canLabor = computed(() => session.can('labor:report'))
const canScan = computed(() => session.can('scan:report'))
const lines = ref<StationLine[]>([])
const openLabor = ref<OpenLabor[]>([])
const cards = ref<StationCard[]>([])
const defectCodes = ref<DefectCode[]>([])
const allDefects = ref<DefectCode[]>([])
const stationId = ref('')
const rows = ref<Row[]>([])
const active = ref<{ row: Row; cardNo: string; remaining: number; lineId: string } | null>(null)
const banner = ref<{ type: 'success' | 'error' | 'warning'; text: string } | null>(null)
const loading = ref(false)
const busy = ref(false)

// 只列出勾選製令途程會經過、且有線別的站點
const stations = computed(() => {
  const names = new Set(props.workOrders.flatMap((w) => w.steps.map((s) => s.station)))
  const list: { id: string; name: string }[] = []
  for (const l of lines.value) if (names.has(l.stationName) && !list.some((s) => s.id === l.stationId)) list.push({ id: l.stationId, name: l.stationName })
  return list
})
const stationLines = computed(() => lines.value.filter((l) => l.stationId === stationId.value))
const running = computed(() => rows.value.filter((r) => r.run))
const pending = computed(() => rows.value.filter((r) => !r.run && r.lineId))
const cardsOf = (woId: string) => cards.value.filter((c) => c.workOrderId === woId)
const sumOf = (woId: string) => cardsOf(woId).reduce((a, c) => a + c.remaining, 0)
const allBusy = computed(() => stationLines.value.every((l) => busyLabel(l.id)))
const lineCode = (id: string) => lines.value.find((l) => l.id === id)?.code ?? ''
const shortCard = (no: string) => no.split('-').slice(-2).join('-')
const time = (v: string) => fmt.time(v, session.factory?.timezone ?? 'Asia/Taipei')
const show = (type: 'success' | 'error' | 'warning', text: string) => (banner.value = { type, text })

// 製令目前所在站：第一個尚未做完的站（第一站應到＝計畫量，之後＝上站良品）
function currentStation(w: WorkOrder) {
  let arrived = w.plannedQty
  for (const s of w.steps) {
    if (s.good + s.defect < arrived || arrived === 0) return s.station
    arrived = s.good
  }
  return w.steps.at(-1)?.station
}

async function init() {
  banner.value = null
  active.value = null
  loading.value = true
  try {
    const [ls, , defects] = await Promise.all([
      api<{ lines: StationLine[] }>('/station/lines'),
      loadLabor(),
      api<DefectCode[]>('/defect-codes').catch(() => []),
    ])
    lines.value = ls.lines
    allDefects.value = defects
    // 預設站點：篩選中的製程站，否則勾選製令最多數目前所在的站
    const count = new Map<string, number>()
    for (const w of props.workOrders) {
      const n = currentStation(w)
      if (n) count.set(n, (count.get(n) ?? 0) + 1)
    }
    const name = props.station || [...count.entries()].sort((a, b) => b[1] - a[1])[0]?.[0]
    stationId.value = stations.value.find((s) => s.name === name)?.id ?? stations.value[0]?.id ?? ''
    rows.value = props.workOrders.map((wo) => ({ wo, lineId: '', headcount: 1, run: null }))
    await onStation()
  } catch (e) {
    show('error', (e as Error).message)
  } finally {
    loading.value = false
  }
}
async function loadLabor() {
  openLabor.value = await api<OpenLabor[]>('/labor/open').catch(() => [])
}
async function loadCards() {
  if (!stationId.value || !rows.value.length) return (cards.value = [])
  cards.value = await api<StationCard[]>(`/station/cards?stationId=${stationId.value}&wo=${rows.value.map((r) => r.wo.id).join(',')}`)
}
// 依站點更新各列：上工中的維持原線；本站沒有可報卡的不分配；其餘依序分配空閒的線，人數帶線別標準人數
function assign() {
  const ids = new Set(stationLines.value.map((l) => l.id))
  const used = new Set<string>()
  for (const r of rows.value) {
    r.run = openLabor.value.find((l) => l.workOrderId === r.wo.id && ids.has(l.lineId)) ?? null
    if (r.run) used.add(r.run.lineId)
    else if (!ids.has(r.lineId) || !cardsOf(r.wo.id).length) r.lineId = ''
  }
  for (const r of rows.value) if (!r.run && r.lineId) used.add(r.lineId)
  const free = stationLines.value.filter((l) => !used.has(l.id) && !openLabor.value.some((o) => o.lineId === l.id))
  for (const r of rows.value) {
    if (r.run || r.lineId || !cardsOf(r.wo.id).length) continue
    const l = free.shift()
    if (l) onLine(r, l.id)
  }
}
function onLine(r: Row, id: string) {
  r.lineId = id
  r.headcount = lines.value.find((l) => l.id === id)?.stdHeadcount || r.headcount || 1
}
function busyLabel(lineId: string, self?: Row) {
  const o = openLabor.value.find((x) => x.lineId === lineId)
  if (o) return '目前 ' + o.woNo
  const r = rows.value.find((x) => x !== self && !x.run && x.lineId === lineId)
  return r ? '已分配 ' + r.wo.woNo : ''
}
async function onStation() {
  active.value = null
  defectCodes.value = allDefects.value.filter((d) => d.isActive && (!d.stationId || d.stationId === stationId.value))
  await loadCards()
  assign()
}
async function refresh() {
  await Promise.all([loadLabor(), loadCards()])
  assign()
}

async function startRows() {
  const list = pending.value
  if (new Set(list.map((r) => r.lineId)).size !== list.length) return show('warning', '同一條線只能上工一張製令，請調整線別')
  busy.value = true
  try {
    await api('/labor/batch-start', {
      method: 'POST',
      body: { items: list.map((r) => ({ lineId: r.lineId, workOrderId: r.wo.id, headcount: r.headcount })) },
    })
    show('success', `已上工 ${list.length} 條線：${list.map((r) => `${lineCode(r.lineId)}（${r.wo.woNo}，${r.headcount} 人）`).join('、')}`)
    await refresh()
  } catch (e) {
    show('error', (e as Error).message)
  } finally {
    busy.value = false
  }
}
async function endRows(list: Row[]) {
  const runs = list.map((r) => r.run!).filter(Boolean)
  try {
    await ElMessageBox.confirm(`下工 ${runs.map((r) => r.lineCode).join('、')}？`, '下工', { confirmButtonText: '確認下工', cancelButtonText: '返回', type: 'warning' })
    for (const r of runs) await api('/labor/end', { method: 'POST', body: { lineId: r.lineId } })
    show('success', `已下工 ${runs.map((r) => r.lineCode).join('、')}`)
    await refresh()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') show('error', (e as Error).message)
  }
}

function lineFor(r: Row) {
  const id = r.run?.lineId ?? r.lineId
  if (!id) show('warning', `${r.wo.woNo} 請先選線別`)
  else if (r.wo.requireLabor && !r.run) show('warning', `${r.wo.woNo} 為強制上工，請先按「上工」`)
  else return id
}
function pick(r: Row, c: StationCard) {
  const lineId = lineFor(r)
  if (lineId) active.value = { row: r, cardNo: c.cardNo, remaining: c.remaining, lineId }
}
function resultText(cardNo: string, res: ScanResult) {
  let text = `✓ ${shortCard(cardNo)} 良品 ${fmt.int(res.goodQty)}`
  if (res.defectQty) text += `、不良 ${fmt.int(res.defectQty)}`
  if (res.rework) text += `（重工 ${fmt.int(res.rework)} 回本站）`
  return text
}
function tail(woNo: string, res: ScanResult, line: string) {
  let text = ''
  if (res.woCompleted) text += ` · 製令 ${woNo} 完工！`
  if (res.laborEnded) text += ` · 本站已報完總數，${line} 自動下工`
  return text
}
async function onReported(res: ScanResult) {
  const a = active.value!
  show('success', resultText(a.cardNo, res) + (res.remaining > 0 ? ` · 本卡還可報 ${fmt.int(res.remaining)}` : '') + tail(a.row.wo.woNo, res, lineCode(a.lineId)))
  active.value = null
  await refresh()
}
// 整張製令在本站的卡全部以良品報完
async function reportAll(r: Row) {
  const lineId = lineFor(r)
  if (!lineId) return
  active.value = null
  busy.value = true
  let total = 0
  let last: ScanResult | null = null
  try {
    for (const c of cardsOf(r.wo.id)) {
      last = { ...(await api<ScanResult>('/scans', { method: 'POST', body: { cardNo: c.cardNo, lineId, goodQty: c.remaining, defects: [], deviceId: `ORDERS-${lineCode(lineId)}` } })), rework: 0 }
      total += c.remaining
    }
    show('success', `✓ ${r.wo.woNo} 本站 ${cardsOf(r.wo.id).length} 張卡全部報完：良品 ${fmt.int(total)}` + (last ? tail(r.wo.woNo, last, lineCode(lineId)) : ''))
  } catch (e) {
    show('error', `已報良品 ${fmt.int(total)} 後中斷：${(e as Error).message}`)
  } finally {
    busy.value = false
    await refresh()
  }
}
</script>

<style scoped>
.head { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; padding-right: 24px; }
.station { width: 150px; }
.small { font-size: 12px; }
.ml { margin-left: 6px; }
.banner { margin-bottom: 10px; }
.running { color: var(--status-good); font-weight: 600; }
.assign { display: flex; align-items: center; gap: 6px; }
.line { width: 190px; }
.head-n { width: 96px; }
.cards { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; }
.cards .el-button { margin-left: 0; }
.report { margin-top: 14px; padding: 14px 16px; border: 1px solid var(--series-1); border-radius: 10px; background: var(--series-1-soft); }
.report-title { margin-bottom: 12px; font-size: 15px; }
</style>
