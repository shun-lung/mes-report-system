<template>
  <div class="grid-wrap">
    <table class="hg">
      <thead>
        <tr>
          <th class="name">站點 / 線別</th>
          <th v-for="(h, i) in hours" :key="h" class="num" :class="{ now: i === currentIdx }">
            {{ hourLabel(h) }}<span v-if="i === currentIdx" class="now-tag">進行中</span>
          </th>
          <th class="num total">良品</th>
          <th class="num total">應達</th>
          <th class="num total">達成</th>
        </tr>
      </thead>
      <tbody>
        <template v-for="st in stations" :key="st.id">
          <tr class="station" @click="toggle(st.id)">
            <td class="name">
              <span class="caret">{{ collapsed.has(st.id) ? '▸' : '▾' }}</span>{{ st.name }}
              <span class="muted small">{{ st.lines.length }} 線</span>
            </td>
            <td v-for="(c, i) in st.hourly" :key="i" :class="cellClass(c, i)"
              @mouseenter="tipCell($event, st.name, c, i)" @mousemove="tipCell($event, st.name, c, i)" @mouseleave="hideTip">
              <template v-if="!isFuture(i) && (c.good || c.target)">
                <span class="glyph">{{ attainmentGlyph(att(c, i)) }}</span>{{ fmt.int(c.good) }}
              </template>
            </td>
            <td class="num total">{{ fmt.int(st.good) }}</td>
            <td class="num total muted">{{ fmt.int(st.targetToDate) }}</td>
            <td class="num total" :class="'ink-' + attainmentClass(ratio(st.good, st.targetToDate))">{{ pct(st.good, st.targetToDate) }}</td>
          </tr>
          <template v-if="!collapsed.has(st.id)">
            <tr v-for="l in st.lines" :key="l.id" class="line">
              <td class="name">
                <span class="dot" :class="l.status" :title="lineStatus[l.status]" />{{ l.name }}
              </td>
              <td v-for="(c, i) in l.hourly" :key="i" :class="cellClass(c, i)"
                @mouseenter="tipCell($event, l.name, c, i)" @mousemove="tipCell($event, l.name, c, i)" @mouseleave="hideTip">
                <template v-if="!isFuture(i) && (c.good || c.target)">
                  <span class="glyph">{{ attainmentGlyph(att(c, i)) }}</span>{{ fmt.int(c.good) }}
                </template>
              </td>
              <td class="num total">{{ fmt.int(l.good) }}</td>
              <td class="num total muted">{{ fmt.int(l.targetToDate) }}</td>
              <td class="num total" :class="'ink-' + attainmentClass(ratio(l.good, l.targetToDate))">{{ pct(l.good, l.targetToDate) }}</td>
            </tr>
          </template>
        </template>
      </tbody>
    </table>
  </div>
  <div class="legend">
    <span class="lg-title">每小時達成率</span>
    <span><i class="sw neg2" />▼ &lt;70%</span>
    <span><i class="sw neg1" />▼ 70–90%</span>
    <span><i class="sw mid" />90–110%</span>
    <span><i class="sw pos1" />▲ 110–130%</span>
    <span><i class="sw pos2" />▲ &gt;130%</span>
    <span class="muted">目標 = 上工時間 × 線別時產能；進行中時段按已過時間折算</span>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive } from 'vue'
import { fmt } from '../../api'
import { hideTip, showTip } from '../tooltip'
import { attainmentClass, attainmentGlyph, type Cell, type Station } from './types'

const props = defineProps<{ hours: string[]; stations: Station[]; now: string; tz: string }>()
const collapsed = reactive(new Set<string>())
const lineStatus = { RUNNING: '運轉中', IDLE: '停滯（30 分鐘無掃描）', OFF: '未開線' }

const nowMs = computed(() => new Date(props.now).getTime())
const currentIdx = computed(() =>
  props.hours.findIndex((h) => {
    const s = new Date(h).getTime()
    return nowMs.value >= s && nowMs.value < s + 3600_000
  }),
)

function toggle(id: string) {
  collapsed.has(id) ? collapsed.delete(id) : collapsed.add(id)
}
function hourLabel(h: string) {
  return fmt.time(h, props.tz).slice(0, 2)
}
function isFuture(i: number) {
  return new Date(props.hours[i]).getTime() > nowMs.value
}
// 進行中時段的目標按已過時間折算
function targetSoFar(c: Cell, i: number) {
  if (i !== currentIdx.value) return c.target
  const frac = (nowMs.value - new Date(props.hours[i]).getTime()) / 3600_000
  return c.target * Math.min(Math.max(frac, 0), 1)
}
// 進行中時段未滿 10 分鐘，不評達成率，避免剛開始就一片紅
function att(c: Cell, i: number) {
  if (i === currentIdx.value && nowMs.value - new Date(props.hours[i]).getTime() < 600_000) return null
  const t = targetSoFar(c, i)
  return t > 0 ? c.good / t : null
}
// 應達量太小（不足 10 分鐘產能）時不評達成率
function ratio(a: number, b: number) {
  return b >= 10 ? a / b : null
}
function pct(a: number, b: number) {
  const r = ratio(a, b)
  return r === null ? '—' : Math.round(r * 100) + '%'
}
function cellClass(c: Cell, i: number) {
  if (isFuture(i)) return 'cell future'
  if (!c.good && !c.target) return 'cell empty'
  return 'cell num ' + attainmentClass(att(c, i))
}
function tipCell(e: MouseEvent, name: string, c: Cell, i: number) {
  if (isFuture(i)) return hideTip()
  const h = hourLabel(props.hours[i])
  const t = targetSoFar(c, i)
  const a = att(c, i)
  showTip(e, `${name} · ${h}:00–${String(+h + 1).padStart(2, '0')}:00${i === currentIdx.value ? '（進行中）' : ''}`, [
    { label: '良品', value: fmt.int(c.good) + ' 雙' },
    { label: '不良', value: fmt.int(c.defect) + ' 雙' },
    { label: i === currentIdx.value ? '目標（折算）' : '目標', value: fmt.int(t) + ' 雙' },
    { label: '達成率', value: a === null ? '—' : fmt.pct(a) },
  ])
}
</script>

<style scoped>
.grid-wrap { overflow-x: auto; }
.hg { width: 100%; border-collapse: separate; border-spacing: 2px; font-size: 13px; }
th { font-weight: 500; color: var(--text-muted); font-size: 12px; padding: 4px 6px; text-align: right; white-space: nowrap; }
th.name, td.name { text-align: left; white-space: nowrap; padding-right: 12px; }
th.now { color: var(--text-primary); }
.now-tag { display: block; font-size: 10px; color: var(--series-1); }
td { padding: 6px 8px; text-align: right; border-radius: 4px; }
tr.station td.name { font-weight: 600; cursor: pointer; }
tr.station { cursor: pointer; }
tr.line td.name { padding-left: 22px; color: var(--text-secondary); }
.caret { display: inline-block; width: 14px; color: var(--text-muted); }
.small { font-size: 11px; font-weight: 400; margin-left: 4px; }
.cell { min-width: 52px; color: var(--text-primary); }
.cell.empty { background: transparent; }
.cell.future {
  background: repeating-linear-gradient(135deg, transparent 0 4px, var(--grid) 4px 5px);
}
.neg2 { background: var(--div-neg-2); }
.neg1 { background: var(--div-neg-1); }
.mid { background: var(--div-mid); }
.pos1 { background: var(--div-pos-1); }
.pos2 { background: var(--div-pos-2); }
.glyph { font-size: 9px; margin-right: 3px; color: var(--text-secondary); }
tr.station .cell { font-weight: 600; }
.total { color: var(--text-primary); padding-left: 10px; }
.ink-neg2, .ink-neg1 { color: var(--div-neg-ink); font-weight: 600; }
.ink-pos1, .ink-pos2 { color: var(--div-pos-ink); font-weight: 600; }
.dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 6px; vertical-align: 1px; }
.dot.RUNNING { background: var(--status-good); }
.dot.IDLE { background: var(--status-warning); }
.dot.OFF { background: var(--status-off); }
.legend { display: flex; flex-wrap: wrap; gap: 6px 14px; margin-top: 10px; font-size: 12px; color: var(--text-secondary); align-items: center; }
.lg-title { color: var(--text-primary); font-weight: 500; }
.sw { display: inline-block; width: 12px; height: 12px; border-radius: 3px; margin-right: 4px; vertical-align: -2px; }
</style>
