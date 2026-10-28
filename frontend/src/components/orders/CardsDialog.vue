<template>
  <el-dialog :model-value="modelValue" :title="`流程卡 — ${woNo}`" width="min(980px, 96vw)" @update:model-value="emit('update:modelValue', $event)">
    <div class="bar">
      <span class="muted small">拆卡：從某站的待做量分出新卡；子卡尚無回報時可合併回原卡。</span>
      <span class="spacer" />
      <el-button size="small" :disabled="!selected.length" @click="print(selected)">列印選取（{{ selected.length }}）</el-button>
      <el-button size="small" type="primary" @click="print([])">列印全部</el-button>
    </div>
    <el-table :data="cards" size="small" max-height="520" :row-class-name="({ row }: any) => (row.status === 'VOID' ? 'is-void' : '')"
      @selection-change="(rows: Card[]) => (selected = rows.map((r) => r.cardNo))">
      <el-table-column type="selection" width="36" :selectable="(r: Card) => r.status !== 'VOID'" />
      <el-table-column label="流程卡" min-width="200">
        <template #default="{ row }">
          <span class="num" :class="{ child: row.parentCardNo }">{{ row.parentCardNo ? '└ ' : '' }}{{ row.cardNo }}</span>
          <el-tooltip v-if="row.note" placement="top" :content="row.note"><span class="note">ⓘ</span></el-tooltip>
          <div class="small muted">
            {{ fmt.int(row.qty) }} 雙 · {{ cardStatus[row.status] }}<template v-if="row.printCount"> · 已印 {{ row.printCount }} 次</template>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="各站（良品／投入，待做）" min-width="330">
        <template #default="{ row }">
          <span v-for="s in row.steps" :key="s.seq" class="step" :class="{ todo: s.remaining > 0 }">
            {{ s.station }} <span class="num">{{ fmt.int(s.good) }}／{{ fmt.int(s.input) }}</span>
            <b v-if="s.remaining > 0" class="num">待 {{ fmt.int(s.remaining) }}</b>
          </span>
        </template>
      </el-table-column>
      <el-table-column v-if="canSplit" label="" width="130">
        <template #default="{ row }">
          <template v-if="row.status !== 'VOID'">
            <el-button v-if="row.steps.some((s: Step) => s.remaining > 0)" size="small" text @click="openSplit(row)">拆卡</el-button>
            <el-button v-if="row.parentCardNo && !row.hasScans" size="small" text type="warning" @click="merge(row)">合併</el-button>
          </template>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="split.open" :title="`拆卡：${split.cardNo}`" width="min(420px, 94vw)" append-to-body>
      <el-form label-width="70px" @submit.prevent>
        <el-form-item label="站點">
          <el-radio-group v-model="split.stepSeq">
            <el-radio v-for="s in split.steps" :key="s.seq" :value="s.seq">{{ s.station }}（待 {{ fmt.int(s.remaining) }}）</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="拆出數量"><el-input-number v-model="split.qty" :min="1" :max="maxQty" /> <span class="muted unit">最多 {{ fmt.int(maxQty) }}</span></el-form-item>
        <el-form-item label="原因" required><el-input v-model="split.reason" maxlength="200" placeholder="例如：部分先送下一站、重工品分開處理" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="split.open = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="doSplit">拆卡</el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, fmt } from '../../api'
import { useSession } from '../../stores/session'

interface Step { seq: number; station: string; input: number; good: number; defect: number; remaining: number }
interface Card { cardNo: string; parentCardNo: string | null; qty: number; status: string; printCount: number; note: string | null; hasScans: boolean; steps: Step[] }

const props = defineProps<{ modelValue: boolean; woId: string; woNo: string }>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; changed: [] }>()
const session = useSession()
const canSplit = computed(() => session.can('scan:report') || session.can('wo:edit'))
const cards = ref<Card[]>([])
const selected = ref<string[]>([])
const busy = ref(false)
const cardStatus: Record<string, string> = { OPEN: '未開始', IN_PROGRESS: '進行中', DONE: '完成', VOID: '作廢' }
const split = reactive({ open: false, cardNo: '', stepSeq: 0, qty: 1, reason: '', steps: [] as Step[] })
const maxQty = computed(() => split.steps.find((s) => s.seq === split.stepSeq)?.remaining ?? 1)

async function load() {
  cards.value = await api<Card[]>(`/work-orders/${props.woId}/card-progress`)
}
watch(() => [props.modelValue, props.woId], () => props.modelValue && load())

function print(cardNos: string[]) {
  window.open(`/print/cards/${props.woId}${cardNos.length ? `?cards=${encodeURIComponent(cardNos.join(','))}` : ''}`, '_blank')
}
function openSplit(c: Card) {
  split.cardNo = c.cardNo
  split.steps = c.steps.filter((s) => s.remaining > 0)
  split.stepSeq = split.steps[0].seq
  split.qty = Math.max(1, Math.floor(split.steps[0].remaining / 2))
  split.reason = ''
  split.open = true
}
async function doSplit() {
  if (!split.reason.trim()) return ElMessage.error('請填寫拆卡原因')
  busy.value = true
  try {
    const r = await api<{ cardNo: string; qty: number }>(`/cards/${encodeURIComponent(split.cardNo)}/split`, {
      method: 'POST', body: { stepSeq: split.stepSeq, qty: split.qty, reason: split.reason.trim() },
    })
    split.open = false
    await load()
    emit('changed')
    await ElMessageBox.confirm(`已拆出 ${r.cardNo}（${fmt.int(r.qty)} 雙）。原卡數量已減少，要列印新卡與原卡嗎？`, '拆卡完成', {
      confirmButtonText: '列印', cancelButtonText: '稍後', type: 'success',
    })
    print([split.cardNo, r.cardNo])
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error((e as Error).message)
  } finally {
    busy.value = false
  }
}
async function merge(c: Card) {
  try {
    await ElMessageBox.confirm(`把 ${c.cardNo}（${fmt.int(c.qty)} 雙）合併回 ${c.parentCardNo}？子卡會作廢。`, '合併子卡', {
      confirmButtonText: '合併', cancelButtonText: '返回', type: 'warning',
    })
    await api(`/cards/${encodeURIComponent(c.cardNo)}/merge`, { method: 'POST', body: {} })
    ElMessage.success('已合併')
    await load()
    emit('changed')
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error((e as Error).message)
  }
}
</script>

<style scoped>
.bar { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; flex-wrap: wrap; }
.spacer { flex: 1; }
.small { font-size: 12px; }
.child { color: var(--text-secondary); }
.note { margin-left: 4px; color: var(--text-muted); cursor: help; }
.step { display: inline-block; margin: 2px 6px 2px 0; padding: 2px 8px; border-radius: 6px; background: var(--surface-2); font-size: 12px; }
.step.todo { background: var(--series-1-soft); }
.step b { margin-left: 4px; }
.unit { margin-left: 8px; }
:deep(.is-void) td { opacity: 0.5; }
</style>
