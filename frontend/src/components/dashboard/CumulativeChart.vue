<template>
  <div>
    <div class="legend">
      <span><i class="ln actual" />實際完工</span>
      <span><i class="ln target" />目標（依上工排定）</span>
    </div>
    <svg :viewBox="`0 0 ${W} ${H}`" class="chart" role="img" :aria-label="aria" @mouseleave="hover = -1; hideTip()">
      <g class="grid">
        <g v-for="t in yTicks" :key="t">
          <line :x1="M.l" :x2="W - M.r" :y1="y(t)" :y2="y(t)" />
          <text :x="M.l - 6" :y="y(t)" dy="0.32em" text-anchor="end">{{ fmt.int(t) }}</text>
        </g>
        <text v-for="(h, i) in labels" :key="i" :x="x(i)" :y="H - M.b + 16" text-anchor="middle">{{ h }}</text>
      </g>
      <line v-if="nowX !== null" class="now" :x1="nowX" :x2="nowX" :y1="M.t" :y2="H - M.b" />
      <text v-if="nowX !== null" class="now-lbl" :x="nowX + 4" :y="H - M.b - 6" text-anchor="start">現在</text>

      <path class="target" :d="path(targetPts)" />
      <path class="actual" :d="path(actualPts)" />
      <circle v-for="(p, i) in actualPts" :key="i" class="mk" :cx="p[0]" :cy="p[1]" r="4" />

      <text v-if="targetPts.length" class="direct" :x="targetPts.at(-1)![0] + 6" :y="targetPts.at(-1)![1]" dy="0.32em">
        目標 {{ fmt.int(targetCum.at(-1)!) }}
      </text>
      <text v-if="actualPts.length" class="direct strong" :x="actualPts.at(-1)![0] + 6" :y="actualPts.at(-1)![1]" dy="0.32em">
        {{ fmt.int(actualCum.at(-1)!) }}
      </text>

      <line v-if="hover >= 0" class="cross" :x1="x(hover)" :x2="x(hover)" :y1="M.t" :y2="H - M.b" />
      <rect class="hit" :x="M.l" :y="M.t" :width="W - M.l - M.r" :height="H - M.t - M.b" @mousemove="onMove" />
    </svg>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { fmt } from '../../api'
import { hideTip, showTip } from '../tooltip'
import type { Cell } from './types'

const props = defineProps<{ hours: string[]; cells: Cell[]; now: string; tz: string }>()
const W = 560
const H = 260
const M = { l: 48, r: 78, t: 18, b: 28 }
const hover = ref(-1)

const n = computed(() => props.hours.length)
const startMs = computed(() => new Date(props.hours[0]).getTime())
const elapsed = computed(() => (new Date(props.now).getTime() - startMs.value) / 3600_000) // 已過小時數
const doneHours = computed(() => Math.max(0, Math.min(n.value, Math.floor(elapsed.value))))

// 累計值：index i = 第 i 個整點邊界
const targetCum = computed(() => {
  const out = [0]
  props.cells.forEach((c) => out.push(out.at(-1)! + c.target))
  return out
})
const actualCum = computed(() => {
  const out = [0]
  for (let i = 0; i < doneHours.value; i++) out.push(out.at(-1)! + props.cells[i].good)
  if (doneHours.value < n.value && elapsed.value > doneHours.value) out.push(out.at(-1)! + props.cells[doneHours.value].good)
  return out
})
const actualX = (i: number) => (i <= doneHours.value ? i : Math.min(elapsed.value, n.value))

const yMax = computed(() => niceMax(Math.max(targetCum.value.at(-1)!, actualCum.value.at(-1)!, 1)))
const yTicks = computed(() => [0, 0.25, 0.5, 0.75, 1].map((f) => f * yMax.value))
const x = (i: number) => M.l + (i / n.value) * (W - M.l - M.r)
const y = (v: number) => H - M.b - (v / yMax.value) * (H - M.t - M.b)

const labels = computed(() => [...props.hours.map((h) => fmt.time(h, props.tz).slice(0, 2)), endLabel()])
function endLabel() {
  const end = new Date(startMs.value + n.value * 3600_000)
  return fmt.time(end, props.tz).slice(0, 2)
}
const targetPts = computed(() => targetCum.value.map((v, i) => [x(i), y(v)] as [number, number]))
const actualPts = computed(() => actualCum.value.map((v, i) => [x(actualX(i)), y(v)] as [number, number]))
const nowX = computed(() => (elapsed.value > 0 && elapsed.value < n.value ? x(elapsed.value) : null))
const aria = computed(
  () => `成品累計完工 ${fmt.int(actualCum.value.at(-1)!)} 雙，本班目標 ${fmt.int(targetCum.value.at(-1)!)} 雙`,
)

function path(pts: [number, number][]) {
  return pts.map((p, i) => (i ? 'L' : 'M') + p[0].toFixed(1) + ',' + p[1].toFixed(1)).join('')
}
function niceMax(v: number) {
  const step = Math.pow(10, Math.floor(Math.log10(v)))
  for (const m of [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10]) if (m * step >= v) return m * step
  return 10 * step
}
function onMove(e: MouseEvent) {
  const svg = (e.currentTarget as SVGRectElement).ownerSVGElement!
  const r = svg.getBoundingClientRect()
  const px = ((e.clientX - r.left) / r.width) * W
  const i = Math.round(((px - M.l) / (W - M.l - M.r)) * n.value)
  hover.value = Math.max(0, Math.min(n.value, i))
  const label = labels.value[hover.value] + ':00'
  const a = hover.value <= doneHours.value ? actualCum.value[hover.value] : null
  const t = targetCum.value[hover.value]
  showTip(e, label, [
    { label: '實際完工累計', value: a === null ? '—（未到）' : fmt.int(a) + ' 雙', swatch: 'var(--series-1)' },
    { label: '目標累計', value: fmt.int(t) + ' 雙', swatch: 'var(--target)' },
    ...(a === null ? [] : [{ label: '差異', value: (a - t >= 0 ? '+' : '') + fmt.int(a - t) + ' 雙' }]),
  ])
}
</script>

<style scoped>
.chart { width: 100%; height: auto; display: block; overflow: visible; }
.legend { display: flex; gap: 16px; font-size: 12px; color: var(--text-secondary); margin-bottom: 4px; }
.ln { display: inline-block; width: 18px; height: 0; border-top: 2px solid; vertical-align: 3px; margin-right: 6px; }
.ln.actual { border-color: var(--series-1); }
.ln.target { border-color: var(--target); border-top-style: dashed; }
.grid line { stroke: var(--grid); stroke-width: 1; }
.grid text { fill: var(--text-muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.target { fill: none; stroke: var(--target); stroke-width: 2; stroke-dasharray: 5 4; }
.actual { fill: none; stroke: var(--series-1); stroke-width: 2; stroke-linejoin: round; }
.mk { fill: var(--series-1); stroke: var(--surface-1); stroke-width: 2; }
.direct { fill: var(--text-secondary); font-size: 12px; font-variant-numeric: tabular-nums; }
.direct.strong { fill: var(--text-primary); font-weight: 600; }
.now { stroke: var(--text-muted); stroke-dasharray: 2 3; }
.now-lbl { fill: var(--text-muted); font-size: 11px; }
.cross { stroke: var(--text-secondary); stroke-width: 1; }
.hit { fill: transparent; cursor: crosshair; }
</style>
