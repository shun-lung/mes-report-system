import { reactive } from 'vue'

export interface TipRow {
  label: string
  value: string
  swatch?: string // CSS 色值，標示對應的圖形
}

// 全頁共用一個圖表提示框
export const tip = reactive({
  visible: false,
  x: 0,
  y: 0,
  title: '',
  rows: [] as TipRow[],
})

export function showTip(e: MouseEvent, title: string, rows: TipRow[]) {
  tip.visible = true
  tip.x = e.clientX
  tip.y = e.clientY
  tip.title = title
  tip.rows = rows
}

export function hideTip() {
  tip.visible = false
}
