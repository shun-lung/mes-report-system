<template>
  <div class="report">
    <div class="qty-row">
      <span class="label">良品</span>
      <el-button :size="size" class="qbtn" @click="goodQty = Math.max(0, goodQty - 5)">−5</el-button>
      <el-button :size="size" class="qbtn" @click="goodQty = Math.max(0, goodQty - 1)">−1</el-button>
      <el-input-number v-model="goodQty" :min="0" :max="remaining" :controls="false" :size="size" class="qty" />
      <el-button :size="size" class="qbtn" @click="goodQty = Math.min(maxGood, goodQty + 1)">＋1</el-button>
      <el-button :size="size" class="qbtn" @click="goodQty = maxGood">全部</el-button>
    </div>

    <div class="defects">
      <span class="label">不良</span>
      <div class="d-list">
        <div v-for="d in defectCodes" :key="d.id" class="d-item" :class="{ active: defects[d.id]?.qty }">
          <button type="button" class="d-btn" @click="addDefect(d.id)">
            {{ d.code }} {{ d.name }}
            <b v-if="defects[d.id]?.qty" class="num">×{{ defects[d.id].qty }}</b>
          </button>
          <template v-if="defects[d.id]?.qty">
            <el-radio-group v-model="defects[d.id].disposition" size="small">
              <el-radio-button value="REWORK">重工</el-radio-button>
              <el-radio-button value="SCRAP">報廢</el-radio-button>
            </el-radio-group>
            <button type="button" class="d-minus" aria-label="減少" @click="removeDefect(d.id)">−</button>
          </template>
        </div>
        <span v-if="!defectCodes.length" class="muted">本站尚未設定不良代碼</span>
      </div>
    </div>

    <div class="confirm-row">
      <span class="sum">合計 <b class="num">{{ fmt.int(total) }}</b> ／ 可報 {{ fmt.int(remaining) }}</span>
      <el-button :size="size" @click="emit('cancel')">取消</el-button>
      <el-button :size="size" type="primary" class="confirm" :loading="busy" :disabled="!canSubmit" @click="submit">{{ confirmText }}</el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api, fmt } from '../../api'

export interface DefectCode { id: string; stationId: string | null; code: string; name: string; isActive: boolean }
export interface ScanResult { goodQty: number; defectQty: number; remaining: number; finalStep: boolean; woCompleted: boolean; laborEnded: boolean; rework: number }

// 流程卡數量回報：良品預設為本站可報量，點不良代碼從良品移一雙到不良
const props = withDefaults(defineProps<{
  cardNo: string; remaining: number; lineId: string; defectCodes: DefectCode[]; deviceId?: string
  size?: 'large' | 'default'; confirmText?: string
}>(), { deviceId: '', size: 'large', confirmText: '確認回報（Enter）' })
const emit = defineEmits<{ done: [ScanResult]; error: [string]; cancel: [] }>()

const goodQty = ref(props.remaining)
const defects = reactive<Record<string, { qty: number; disposition: 'REWORK' | 'SCRAP' }>>({})
const busy = ref(false)
const defectTotal = computed(() => Object.values(defects).reduce((a, d) => a + d.qty, 0))
const total = computed(() => goodQty.value + defectTotal.value)
const maxGood = computed(() => Math.max(0, props.remaining - defectTotal.value))
const canSubmit = computed(() => total.value > 0 && total.value <= props.remaining)

watch(() => [props.cardNo, props.remaining], () => {
  goodQty.value = props.remaining
  for (const k of Object.keys(defects)) delete defects[k]
})

function addDefect(id: string) {
  if (goodQty.value > 0) goodQty.value--
  else if (total.value >= props.remaining) return
  defects[id] = { qty: (defects[id]?.qty ?? 0) + 1, disposition: defects[id]?.disposition ?? 'REWORK' }
}
function removeDefect(id: string) {
  const d = defects[id]
  if (!d) return
  d.qty--
  goodQty.value++
  if (d.qty <= 0) delete defects[id]
}

async function submit() {
  if (!canSubmit.value || busy.value) return
  busy.value = true
  try {
    const res = await api<Omit<ScanResult, 'rework'>>('/scans', {
      method: 'POST',
      body: {
        cardNo: props.cardNo,
        lineId: props.lineId,
        goodQty: goodQty.value,
        defects: Object.entries(defects).map(([defectCodeId, d]) => ({ defectCodeId, qty: d.qty, disposition: d.disposition })),
        deviceId: props.deviceId,
      },
    })
    const rework = Object.values(defects).filter((d) => d.disposition === 'REWORK').reduce((a, d) => a + d.qty, 0)
    emit('done', { ...res, rework })
  } catch (e) {
    emit('error', (e as Error).message)
  } finally {
    busy.value = false
  }
}

defineExpose({ submit, canSubmit })
</script>

<style scoped>
.report { display: flex; flex-direction: column; gap: 16px; }
.qty-row, .defects, .confirm-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.qty-row .el-button, .confirm-row .el-button { margin-left: 0; }
.label { width: 48px; font-weight: 600; }
.qty { width: 140px; }
.qty :deep(input) { font-size: 26px; font-weight: 700; text-align: center; color: var(--text-primary); }
.qbtn { min-width: 64px; }
.defects { align-items: flex-start; }
.d-list { flex: 1; display: flex; flex-wrap: wrap; gap: 8px; }
.d-item { display: flex; align-items: center; gap: 6px; border: 1px solid var(--border); border-radius: 10px; padding: 4px; }
.d-item.active { border-color: var(--status-critical); }
.d-btn, .d-minus { border: 0; background: transparent; color: var(--text-primary); font: inherit; padding: 8px 10px; cursor: pointer; border-radius: 8px; }
.d-btn:hover, .d-minus:hover { background: var(--surface-2); }
.d-minus { font-size: 20px; line-height: 1; padding: 6px 12px; }
.confirm-row { justify-content: flex-end; border-top: 1px solid var(--grid); padding-top: 14px; }
.sum { margin-right: auto; color: var(--text-secondary); }
.sum b { font-size: 22px; color: var(--text-primary); }
.confirm { min-width: 200px; height: 52px; font-size: 18px; }
</style>
