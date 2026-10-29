<template>
  <div class="station">
    <header class="top">
      <router-link to="/" class="back" title="回系統">←</router-link>
      <div class="title">現場掃描</div>
      <el-select v-model="lineId" placeholder="選擇本機線別" size="large" class="line-select" @change="onLineChange">
        <el-option-group v-for="g in lineGroups" :key="g.station" :label="g.station">
          <el-option v-for="l in g.lines" :key="l.id" :label="`${l.code}　${l.name}`" :value="l.id" />
        </el-option-group>
      </el-select>
      <el-tag v-if="restricted" size="small" type="info">只列出你被授權的線別</el-tag>
      <span v-if="state" class="muted">{{ state.shift }} · 生產日 {{ state.workDate }}</span>
      <span class="spacer" />
      <span class="muted">{{ session.user?.name }}</span>
    </header>

    <el-result v-if="!canScan" icon="warning" title="此帳號沒有現場回報權限" sub-title="請以現場組長帳號登入" />
    <el-result v-else-if="!lineId" icon="info" title="請先選擇這台裝置所屬的線別" sub-title="選擇後會記住，之後開啟就是這條線" />

    <div v-else-if="state" class="body">
      <!-- 左：線別狀況 -->
      <aside class="side">
        <section class="panel">
          <div class="panel-title">{{ state.line.stationName }} · {{ state.line.code }}</div>
          <template v-if="state.session">
            <div class="status on"><i />上工中</div>
            <div class="wo">{{ state.session.woNo }}</div>
            <div class="muted">{{ state.session.itemNo }} {{ state.session.itemName }}</div>
            <div class="kv"><span>人數</span><b class="num">{{ state.session.headcount }}</b></div>
            <div class="kv"><span>上工時間</span><b class="num">{{ time(state.session.startedAt) }}</b></div>
          </template>
          <div v-else class="status off"><i />未上工</div>
          <div class="btns">
            <el-button size="large" type="primary" @click="openLabor()">{{ state.session ? '換製令／人數' : '上工' }}</el-button>
            <el-popconfirm v-if="state.session" title="確定下工？" @confirm="endLabor">
              <template #reference><el-button size="large">下工</el-button></template>
            </el-popconfirm>
          </div>
        </section>

        <section class="panel totals">
          <div><span>本班良品</span><b class="num">{{ fmt.int(state.totals.good) }}</b></div>
          <div><span>不良</span><b class="num bad">{{ fmt.int(state.totals.defect) }}</b></div>
          <div><span>回報次數</span><b class="num">{{ state.totals.scans }}</b></div>
        </section>

        <section class="panel recent">
          <div class="panel-title">最近回報</div>
          <div v-for="r in state.recent" :key="r.id" class="r-row" :class="{ reversed: r.reversed, reversal: r.reversalOf }">
            <span class="num muted">{{ time(r.at) }}</span>
            <span class="num">{{ shortCard(r.cardNo) }}</span>
            <span class="num q">{{ r.good >= 0 ? '+' : '' }}{{ fmt.int(r.good) }}<span v-if="r.defect" class="bad"> ✕{{ fmt.int(Math.abs(r.defect)) }}</span></span>
            <span v-if="r.reversalOf" class="tag">撤銷</span>
            <span v-else-if="r.reversed" class="tag muted">已撤銷</span>
            <button v-else type="button" class="undo" @click="undo(r)">撤銷</button>
          </div>
          <div v-if="!state.recent.length" class="muted">本班尚無回報</div>
        </section>
      </aside>

      <!-- 右：掃描與回報 -->
      <main class="main">
        <div class="scan-box" :class="{ ready: !card }">
          <input ref="scanInput" v-model="scanText" class="scan-input" placeholder="請掃描流程卡條碼（也可輸入製令號）"
            autocomplete="off" autocapitalize="off" spellcheck="false" @keydown.enter.prevent="onEnter" />
          <el-button size="large" :disabled="!scanText.trim()" @click="lookup(scanText)">查詢</el-button>
        </div>

        <div v-if="banner" class="banner" :class="banner.type" role="status">{{ banner.text }}</div>

        <section v-if="picks" class="picks">
          <div class="picks-title">製令 {{ picks.woNo }} 在本站有 {{ picks.pick.length }} 張流程卡可報，請選擇：</div>
          <div class="picks-list">
            <el-button v-for="p in picks.pick" :key="p.cardNo" size="large" @click="lookup(p.cardNo)">
              <span class="num">{{ p.cardNo.slice(picks.woNo.length + 1) || p.cardNo }}</span>
              <small class="muted">&nbsp;可報 {{ p.remaining }}／{{ p.qty }}</small>
            </el-button>
          </div>
        </section>

        <section v-if="card" class="card-panel">
          <div class="card-head">
            <img v-if="card.imageId" :src="`/api/images/${card.imageId}`" class="thumb" alt="" />
            <div class="card-info">
              <div class="card-no num">{{ card.cardNo }}</div>
              <div class="item">{{ card.itemNo }} {{ card.itemName }}</div>
              <div v-if="card.requireAuth || card.requireLabor || card.requireIssue" class="ctl">
                <el-tag size="small" type="warning">生管管制</el-tag>
                <span v-if="card.requireAuth" class="small">人員授權{{ state?.authorized ? ' ✓' : '（你未被授權此線別）' }}</span>
                <span v-if="card.requireLabor" class="small">強制上工{{ state?.session?.woId === card.woId ? ' ✓' : '（須先上工）' }}</span>
                <span v-if="card.requireIssue" class="small">強制發料（本站用料須已發完）</span>
              </div>
              <div class="muted">製令 {{ card.woNo }} · 本卡 {{ fmt.int(card.cardQty) }} {{ card.unit }}<template v-if="card.parentCardNo"> · 拆自 {{ shortCard(card.parentCardNo) }}</template></div>
            </div>
            <div class="remain">
              <span>本站可報</span>
              <b class="num">{{ fmt.int(card.remaining) }}</b>
              <el-button v-if="card.remaining > 1" size="small" text class="split-btn" @click="openSplit">拆卡</el-button>
            </div>
          </div>

          <div class="steps">
            <span v-for="s in card.steps" :key="s.seq" class="step" :class="{ here: s.here }">
              {{ s.station }}<small class="num">良品 {{ fmt.int(s.good) }}／投入 {{ fmt.int(s.input) }}</small>
            </span>
          </div>

          <CardReport v-if="card.onRoute && card.remaining > 0" ref="report" :card-no="card.cardNo" :remaining="card.remaining"
            :line-id="lineId" :defect-codes="defectCodes" :device-id="`STATION-${state.line.code}`"
            @done="onReported" @error="(m) => show('err', m)" @cancel="clearCard" />
        </section>
      </main>
    </div>

    <el-dialog v-model="labor.open" :title="labor.title" width="min(460px, 94vw)" @closed="focusScan">
      <p v-if="labor.reason" class="reason">{{ labor.reason }}</p>
      <el-form label-width="70px" size="large" @submit.prevent>
        <el-form-item label="製令">
          <el-select v-model="labor.woId" placeholder="選擇製令">
            <el-option v-for="w in state?.workOrders ?? []" :key="w.id" :label="`${w.woNo}　${w.itemNo}`" :value="w.id">
              <span>{{ w.woNo }} {{ w.itemNo }}</span>
              <span class="opt-r">可報 {{ fmt.int(w.remaining) }}<template v-if="w.upstream">（上游 {{ fmt.int(w.upstream) }}）</template></span>
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="人數"><el-input-number v-model="labor.headcount" :min="1" :max="99" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button v-if="labor.skippable" size="large" @click="skipLabor">先不上工，直接回報</el-button>
        <el-button size="large" type="primary" :loading="busy" :disabled="!labor.woId" @click="startLabor">上工</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import CardReport, { type DefectCode, type ScanResult } from './CardReport.vue'
import { ElMessageBox } from 'element-plus'
import { api, fmt } from '../../api'
import { useSession } from '../../stores/session'

interface Line { id: string; code: string; name: string; stationId: string; stationName: string; capacityPerHour: number; stdHeadcount: number }
interface State {
  line: Line; shift: string; workDate: string; authorized: boolean
  session: { woId: string; woNo: string; itemNo: string; itemName: string; headcount: number; startedAt: string } | null
  totals: { good: number; defect: number; scans: number }
  recent: { id: string; at: string; cardNo: string; woNo: string; good: number; defect: number; user: string; reversalOf: string | null; reversed: boolean }[]
  workOrders: { id: string; woNo: string; itemNo: string; itemName: string; plannedQty: number; remaining: number; doneHere: number; upstream: number }[]
}
interface Card {
  cardNo: string; cardQty: number; status: string; woId: string; woNo: string; woStatus: string; itemNo: string; itemName: string; unit: string
  imageId: string | null; onRoute: boolean; remaining: number; stepSeq: number; parentCardNo: string | null
  requireAuth: boolean; requireLabor: boolean; requireIssue: boolean
  steps: { seq: number; station: string; input: number; good: number; defect: number; remaining: number; here: boolean }[]
}

const session = useSession()
const canScan = computed(() => session.can('scan:report'))
const storeKey = computed(() => `mes.station.line.${session.factoryId}`)
interface StationLine { id: string; code: string; name: string; stationName: string; authorized: boolean }
const lines = ref<StationLine[]>([])
const restricted = ref(false)
const allDefects = ref<DefectCode[]>([])
const lineId = ref('')
const state = ref<State | null>(null)
const scanText = ref('')
const scanInput = ref<HTMLInputElement>()
const card = ref<Card | null>(null)
const report = ref<InstanceType<typeof CardReport>>()
const banner = ref<{ type: 'ok' | 'err' | 'warn'; text: string } | null>(null)
// 掃到製令號且本站有多張卡可報時，列出讓現場挑選
const picks = ref<{ woNo: string; pick: { cardNo: string; qty: number; remaining: number }[] } | null>(null)
const busy = ref(false)
const labor = reactive({ open: false, title: '', reason: '', woId: '', headcount: 1, skippable: false })
let refreshTimer: number | undefined

const lineGroups = computed(() => {
  const groups: { station: string; lines: StationLine[] }[] = []
  for (const l of lines.value) {
    const g = groups.find((x) => x.station === l.stationName)
    if (g) g.lines.push(l)
    else groups.push({ station: l.stationName, lines: [l] })
  }
  return groups
})
const defectCodes = computed(() =>
  allDefects.value.filter((d) => d.isActive && (!d.stationId || d.stationId === state.value?.line.stationId)),
)

const time = (v: string) => fmt.time(v, session.factory?.timezone ?? 'Asia/Taipei')
const shortCard = (no: string) => no.split('-').slice(-2).join('-')

function readLine() {
  try {
    return localStorage.getItem(storeKey.value) ?? ''
  } catch {
    return ''
  }
}
function saveLine(id: string) {
  try {
    localStorage.setItem(storeKey.value, id)
  } catch {
    /* 無法保存時每次開啟需重選 */
  }
}

// 現場回饋音：成功短高音、錯誤兩聲低音
function beep(ok: boolean) {
  try {
    const ctx = new AudioContext()
    const play = (freq: number, at: number) => {
      const o = ctx.createOscillator()
      const g = ctx.createGain()
      o.frequency.value = freq
      g.gain.value = 0.08
      o.connect(g).connect(ctx.destination)
      o.start(ctx.currentTime + at)
      o.stop(ctx.currentTime + at + 0.12)
    }
    if (ok) play(1320, 0)
    else { play(330, 0); play(330, 0.18) }
  } catch {
    /* 不支援音效時略過 */
  }
}
function show(type: 'ok' | 'err' | 'warn', text: string) {
  banner.value = { type, text }
  if (type !== 'warn') beep(type === 'ok')
}
function focusScan() {
  nextTick(() => scanInput.value?.focus())
}

async function loadState() {
  if (!lineId.value) return
  try {
    state.value = await api<State>(`/station/state?lineId=${lineId.value}`)
  } catch (e) {
    show('err', (e as Error).message)
  }
}
async function onLineChange(id: string) {
  saveLine(id)
  clearCard()
  await loadState()
  focusScan()
}

async function onEnter() {
  const text = scanText.value.trim()
  if (text) return lookup(text)
  // 空白 Enter：已帶出流程卡時視為確認回報
  if (report.value?.canSubmit) report.value.submit()
}

async function lookup(raw: string) {
  const cardNo = raw.trim()
  scanText.value = ''
  if (!cardNo) return
  clearCard()
  try {
    const res = await api<Card | NonNullable<typeof picks.value>>(`/cards/${encodeURIComponent(cardNo)}?lineId=${lineId.value}`)
    if ('pick' in res) {
      picks.value = res
      banner.value = null
      return
    }
    const c = res
    if (c.status === 'VOID') {
      show('err', `流程卡 ${c.cardNo} 已作廢（已合併回原卡）`)
      return focusScan()
    }
    if (!c.onRoute) {
      show('err', `此流程卡的途程不經過「${state.value?.line.stationName}」`)
      return focusScan()
    }
    if (c.woStatus !== 'RELEASED' && c.woStatus !== 'IN_PROGRESS') {
      show('err', `製令 ${c.woNo} 狀態不可回報`)
      return focusScan()
    }
    card.value = c
    if (c.remaining <= 0) {
      show('warn', '本站沒有可報數量：上站尚未完成，或本站已報完')
      return focusScan()
    }
    banner.value = null
    const s = state.value?.session
    if (!s || s.woId !== c.woId) {
      const must = c.requireLabor ? '（此製令為生管管制，須先上工才能回報）' : ''
      openLabor(c, (s ? `本線目前生產 ${s.woNo}，這張卡屬於 ${c.woNo}。要換製令嗎？` : `本線尚未上工。要以 ${c.woNo} 上工嗎？`) + must)
      return
    }
    focusScan()
  } catch (e) {
    show('err', (e as Error).message)
    focusScan()
  }
}

function clearCard() {
  picks.value = null
  card.value = null
}

async function onReported(res: ScanResult) {
  let text = `✓ ${shortCard(card.value!.cardNo)} 已回報：良品 ${fmt.int(res.goodQty)}`
  if (res.defectQty) text += `、不良 ${fmt.int(res.defectQty)}`
  if (res.rework) text += `（重工 ${fmt.int(res.rework)} 雙回到本站待重報）`
  text += res.remaining > 0 ? ` · 本站還可報 ${fmt.int(res.remaining)}` : ' · 本站已報完'
  if (res.finalStep) text += ' · 已完工入庫'
  if (res.woCompleted) text += ` · 製令 ${card.value!.woNo} 完工！`
  if (res.laborEnded) text += ` · 本站數量已達總數，${state.value?.line.code} 已自動下工`
  show('ok', text)
  clearCard()
  await loadState()
  focusScan()
}

async function undo(r: State['recent'][number]) {
  try {
    const { value } = await ElMessageBox.prompt(
      `撤銷 ${shortCard(r.cardNo)} 的這筆回報（良品 ${fmt.int(r.good)}、不良 ${fmt.int(r.defect)}）？`,
      '撤銷回報',
      { confirmButtonText: '撤銷', cancelButtonText: '返回', type: 'warning', inputPlaceholder: '撤銷原因（必填）',
        inputValidator: (v) => (v && v.trim() ? true : '請填寫撤銷原因') },
    )
    const res = await api<{ goodQty: number; remaining: number }>(`/scans/${r.id}/reverse`, { method: 'POST', body: { reason: value.trim() } })
    show('ok', `✓ 已撤銷 ${shortCard(r.cardNo)}：良品 ${fmt.int(res.goodQty)} 已退回，本站可報 ${fmt.int(res.remaining)}`)
    clearCard()
    await loadState()
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') show('err', (e as Error).message)
  }
  focusScan()
}

// 拆卡：從本站待做量分出新卡（例如部分先送下一站），拆完可列印新卡
async function openSplit() {
  const c = card.value
  if (!c) return
  try {
    const { value } = await ElMessageBox.prompt(
      `從 ${shortCard(c.cardNo)} 在「${state.value?.line.stationName}」的待做 ${fmt.int(c.remaining)} 雙中，拆出幾雙成為新卡？`,
      '拆卡', { confirmButtonText: '下一步', cancelButtonText: '取消', inputValue: String(Math.floor(c.remaining / 2)),
        inputValidator: (v) => { const n = Number(v); return n > 0 && n <= c.remaining && Number.isInteger(n) ? true : `請輸入 1～${c.remaining}` } },
    )
    const qty = Number(value)
    const { value: reason } = await ElMessageBox.prompt('拆卡原因', '拆卡', {
      confirmButtonText: '拆卡', cancelButtonText: '取消', inputPlaceholder: '例如：部分先送下一站',
      inputValidator: (v) => (v && v.trim() ? true : '請填寫原因'),
    })
    const r = await api<{ cardNo: string; qty: number; workOrderId: string }>(`/cards/${encodeURIComponent(c.cardNo)}/split`, {
      method: 'POST', body: { stepSeq: c.stepSeq, qty, reason: reason.trim() },
    })
    window.open(`/print/cards/${r.workOrderId}?cards=${encodeURIComponent([c.cardNo, r.cardNo].join(','))}`, '_blank')
    await lookup(c.cardNo) // 重新帶出原卡（會重設提示），之後再顯示拆卡結果
    show('ok', `✓ 已拆出 ${shortCard(r.cardNo)}（${fmt.int(r.qty)} 雙），請列印新流程卡；原卡本站還可報 ${fmt.int(card.value?.remaining ?? 0)}`)
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') show('err', (e as Error).message)
  }
  focusScan()
}

function openLabor(c?: Card, reason = '') {
  const s = state.value?.session
  labor.title = c ? '上工確認' : s ? '換製令／調整人數' : '上工'
  labor.reason = reason
  labor.woId = c?.woId ?? s?.woId ?? state.value?.workOrders.find((w) => w.remaining > 0)?.id ?? ''
  labor.headcount = s?.headcount ?? (state.value?.line.stdHeadcount || 1)
  labor.skippable = !!c && !c.requireLabor
  labor.open = true
}
async function startLabor() {
  busy.value = true
  try {
    await api('/labor/start', { method: 'POST', body: { lineId: lineId.value, workOrderId: labor.woId, headcount: labor.headcount } })
    labor.open = false
    show('ok', '✓ 已上工')
    await loadState()
  } catch (e) {
    show('err', (e as Error).message)
  } finally {
    busy.value = false
  }
}
function skipLabor() {
  labor.open = false
}
async function endLabor() {
  try {
    await api('/labor/end', { method: 'POST', body: { lineId: lineId.value } })
    show('ok', '✓ 已下工')
    await loadState()
  } catch (e) {
    show('err', (e as Error).message)
  }
  focusScan()
}

// 點畫面空白處時把游標拉回掃描欄，避免掃描槍輸入落空
function refocus(e: MouseEvent) {
  const t = e.target as HTMLElement
  if (!labor.open && !t.closest('input, textarea, .el-select, .el-input-number, button, .el-radio-group, .el-popper')) focusScan()
}

onMounted(async () => {
  if (!canScan.value) return
  const [ls, defects] = await Promise.all([
    api<{ lines: StationLine[]; restricted: boolean }>('/station/lines'),
    api<DefectCode[]>('/defect-codes'),
  ])
  lines.value = ls.lines
  restricted.value = ls.restricted
  allDefects.value = defects
  const saved = readLine()
  if (saved && lines.value.some((l) => l.id === saved)) lineId.value = saved
  else if (lines.value.length === 1) {
    lineId.value = lines.value[0].id
    saveLine(lineId.value)
  }
  await loadState()
  focusScan()
  refreshTimer = window.setInterval(() => !busy.value && loadState(), 30_000)
  document.addEventListener('click', refocus)
})
onUnmounted(() => {
  window.clearInterval(refreshTimer)
  document.removeEventListener('click', refocus)
})
</script>

<style scoped>
.station { min-height: 100%; display: flex; flex-direction: column; background: var(--surface-0); font-size: 16px; }
.top { display: flex; align-items: center; gap: 14px; padding: 10px 16px; background: var(--surface-1); border-bottom: 1px solid var(--border); flex-wrap: wrap; }
.back { font-size: 22px; text-decoration: none; color: var(--text-secondary); padding: 0 4px; }
.title { font-size: 20px; font-weight: 700; }
.line-select { width: 260px; }
.spacer { flex: 1; }
.body { flex: 1; display: grid; grid-template-columns: 320px 1fr; gap: 16px; padding: 16px; align-items: start; }
.side { display: flex; flex-direction: column; gap: 12px; }
.panel { background: var(--surface-1); border: 1px solid var(--border); border-radius: 12px; padding: 16px; }
.panel-title { font-weight: 600; margin-bottom: 8px; }
.status { display: flex; align-items: center; gap: 8px; font-weight: 600; margin-bottom: 6px; }
.status i { width: 10px; height: 10px; border-radius: 50%; }
.status.on i { background: var(--status-good); }
.status.off i { background: var(--status-off); }
.wo { font-size: 18px; font-weight: 600; font-variant-numeric: tabular-nums; }
.kv { display: flex; justify-content: space-between; margin-top: 6px; color: var(--text-secondary); }
.kv b { color: var(--text-primary); }
.btns { display: flex; gap: 8px; margin-top: 14px; }
.btns .el-button { flex: 1; }
.totals { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; text-align: center; }
.totals span { display: block; font-size: 13px; color: var(--text-secondary); }
.totals b { font-size: 24px; }
.bad { color: var(--div-neg-ink); }
.recent { font-size: 14px; }
.r-row { display: grid; grid-template-columns: 44px 1fr auto 48px; gap: 6px; align-items: center; padding: 4px 0; border-bottom: 1px solid var(--grid); }
.r-row.reversed .q { text-decoration: line-through; color: var(--text-muted); }
.r-row.reversal { color: var(--text-secondary); }
.tag { font-size: 12px; text-align: center; }
.undo { border: 0; background: transparent; color: var(--div-neg-ink); font: inherit; font-size: 13px; cursor: pointer; padding: 2px 4px; border-radius: 4px; }
.undo:hover { background: var(--surface-2); }

.main { display: flex; flex-direction: column; gap: 12px; min-width: 0; }
.scan-box { display: flex; gap: 10px; }
.scan-input {
  flex: 1; min-width: 0; font-size: 24px; padding: 14px 18px; border-radius: 12px; border: 2px solid var(--border);
  background: var(--surface-1); color: var(--text-primary); font-variant-numeric: tabular-nums;
}
.picks { margin-top: 12px; padding: 14px; border: 1px solid var(--border); border-radius: 10px; background: var(--surface-1); }
.picks-title { font-weight: 600; margin-bottom: 10px; }
.picks-list { display: flex; flex-wrap: wrap; gap: 8px; }
.picks-list .el-button { margin-left: 0; }
.scan-input:focus { outline: none; border-color: var(--series-1); box-shadow: 0 0 0 4px var(--series-1-soft); }
.banner { padding: 14px 18px; border-radius: 10px; font-size: 18px; font-weight: 500; }
.banner.ok { background: color-mix(in srgb, var(--status-good) 14%, var(--surface-1)); border: 1px solid var(--status-good); }
.banner.err { background: color-mix(in srgb, var(--status-critical) 12%, var(--surface-1)); border: 1px solid var(--status-critical); }
.banner.warn { background: color-mix(in srgb, var(--status-warning) 16%, var(--surface-1)); border: 1px solid var(--status-warning); }

.card-panel { background: var(--surface-1); border: 1px solid var(--border); border-radius: 12px; padding: 18px; display: flex; flex-direction: column; gap: 16px; }
.card-head { display: flex; gap: 16px; align-items: center; }
.thumb { width: 72px; height: 72px; border-radius: 8px; object-fit: cover; }
.card-info { flex: 1; min-width: 0; }
.card-no { font-size: 22px; font-weight: 700; }
.item { font-size: 18px; margin: 2px 0; }
.remain { text-align: center; padding: 8px 16px; border-radius: 10px; background: var(--series-1-soft); }
.remain span { display: block; font-size: 13px; color: var(--text-secondary); }
.remain b { font-size: 36px; }
.split-btn { display: block; margin: 2px auto 0; }
.steps { display: flex; gap: 6px; flex-wrap: wrap; }
.step { padding: 6px 12px; border-radius: 8px; background: var(--surface-2); color: var(--text-secondary); font-size: 14px; display: flex; flex-direction: column; line-height: 1.3; }
.step.here { background: var(--series-1); color: #fff; font-weight: 600; }
.step small { font-size: 12px; opacity: 0.85; }
.ctl { display: flex; align-items: center; gap: 8px; margin: 2px 0; flex-wrap: wrap; }
.ctl .small { font-size: 13px; color: var(--text-secondary); }
.reason { margin: 0 0 12px; font-size: 16px; }
.opt-r { float: right; color: var(--text-muted); font-size: 12px; margin-left: 12px; }
@media (max-width: 900px) {
  .body { grid-template-columns: 1fr; }
  .side { order: 2; }
  .line-select { width: 100%; }
}
</style>
