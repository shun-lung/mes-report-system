export interface Cell { good: number; defect: number; target: number }
export interface Line {
  id: string; code: string; name: string; capacity: number
  hourly: Cell[]; good: number; defect: number; target: number; targetToDate: number
  manHours: number; earnedHours: number; headcount: number
  currentWo: string | null; currentItem: string | null; lastScanAt: string | null
  status: 'RUNNING' | 'IDLE' | 'OFF'
}
export interface Station {
  id: string; code: string; name: string
  hourly: Cell[]; good: number; defect: number; target: number; targetToDate: number; wip: number
  isFinal: boolean
  lines: Line[]
}
export interface StepProgress { seq: number; station: string; input: number; good: number; defect: number }
export interface WorkOrder {
  id: string; woNo: string; status: string; source: string; itemNo: string; itemName: string; category: string
  plannedQty: number; finishedQty: number; cards: number; dueDate: string | null
  soNo: string | null; customer: string | null; parentWoNo: string | null
  releasedAt: string | null; completedAt: string | null; steps: StepProgress[]
  requireAuth: boolean; requireLabor: boolean; requireIssue: boolean
  issue: { items: number; done: number; arrival: number }
}
export interface ShiftDashboard {
  factory: { code: string; name: string; timezone: string }
  shift: { id: string; code: string; name: string; workDate: string; start: string; end: string }
  shifts: { id: string; code: string; name: string }[]
  prev: { shiftId: string; date: string }
  next: { shiftId: string; date: string }
  now: string
  hours: string[]
  kpi: {
    fgOutput: number; sfgOutput: number; fgTarget: number; fgTargetToDate: number
    moves: number; defects: number; yield: number; manHours: number; earnedHours: number; efficiency: number
    wip: number; pendingAlerts: number; runningLines: number; totalLines: number
  }
  stations: Station[]
  workOrders: WorkOrder[]
  defects: { code: string; name: string; station: string; qty: number }[]
  recent: { at: string; line: string; station: string; woNo: string; cardNo: string; itemNo: string; good: number; defect: number; user: string }[]
}

export const statusText: Record<string, string> = {
  DRAFT: '草稿', RELEASED: '已開立', SCHEDULED: '已排程', IN_PROGRESS: '生產中', COMPLETED: '完工', CLOSED: '結案', CANCELLED: '取消',
}

// 達成率 → 雙向色階級距與方向符號
export function attainmentClass(att: number | null): string {
  if (att === null) return 'mid'
  if (att < 0.7) return 'neg2'
  if (att < 0.9) return 'neg1'
  if (att <= 1.1) return 'mid'
  if (att <= 1.3) return 'pos1'
  return 'pos2'
}
export function attainmentGlyph(att: number | null): string {
  if (att === null) return ''
  if (att < 0.9) return '▼'
  if (att > 1.1) return '▲'
  return ''
}
