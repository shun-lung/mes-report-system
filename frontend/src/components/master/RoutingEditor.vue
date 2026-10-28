<template>
  <div>
    <div class="bar">
      <span class="muted small">途程建立後不可修改；要調整請建立新版本。已開立的製令維持開立當時的版本。</span>
      <el-button v-if="canEdit" type="primary" size="small" @click="openNew(versions.find((v) => v.isActive))">建立新版本</el-button>
    </div>

    <el-empty v-if="!versions.length" description="尚無途程，無法開立製令" :image-size="60" />
    <div v-for="v in versions" :key="v.id" class="ver" :class="{ active: v.isActive }">
      <div class="ver-head">
        <b>v{{ v.version }}</b>
        <el-tag v-if="v.isActive" size="small" type="success">生效中</el-tag>
        <span class="muted small">{{ v.note }} · {{ v.workOrders }} 張製令使用 · {{ v.createdAt.slice(0, 10) }}</span>
        <span class="spacer" />
        <template v-if="canEdit">
          <el-popconfirm v-if="!v.isActive" :title="`將 v${v.version} 設為生效？之後新開的製令會用此版本。`" width="240" @confirm="activate(v.id)">
            <template #reference><el-button size="small" text>設為生效</el-button></template>
          </el-popconfirm>
          <el-button size="small" text @click="openNew(v)">以此建新版</el-button>
        </template>
      </div>
      <div class="flow">
        <template v-for="(s, i) in v.steps" :key="s.seq">
          <span v-if="i" class="arrow">→</span>
          <span class="step"><b>{{ s.stationName }}</b><small class="num">{{ s.stdSecondsPerUnit }} 人秒</small></span>
        </template>
      </div>
    </div>

    <el-dialog v-model="dlg.open" title="建立途程新版本" width="min(560px, 96vw)" append-to-body>
      <div class="hint top">標準工時以「人秒／雙」計：線別人數 × 3600 ÷ 時產能。例：10 人、每小時 60 雙 → 600 人秒。</div>
      <div v-for="(s, i) in dlg.steps" :key="i" class="row">
        <span class="seq num">{{ (i + 1) * 10 }}</span>
        <el-select v-model="s.stationId" placeholder="站點" class="st">
          <el-option v-for="st in stations" :key="st.id" :label="st.name" :value="st.id"
            :disabled="dlg.steps.some((x, j) => j !== i && x.stationId === st.id)" />
        </el-select>
        <el-input-number v-model="s.stdSecondsPerUnit" :min="0" :step="10" controls-position="right" class="sec" />
        <span class="muted small">人秒</span>
        <el-button size="small" text :disabled="i === 0" @click="move(i, -1)">↑</el-button>
        <el-button size="small" text :disabled="i === dlg.steps.length - 1" @click="move(i, 1)">↓</el-button>
        <el-button size="small" text type="danger" :disabled="dlg.steps.length === 1" @click="dlg.steps.splice(i, 1)">✕</el-button>
      </div>
      <el-button size="small" @click="dlg.steps.push({ stationId: '', stdSecondsPerUnit: 0 })">＋ 加站點</el-button>
      <el-form label-width="70px" class="mt" @submit.prevent>
        <el-form-item label="說明"><el-input v-model="dlg.note" maxlength="500" placeholder="例如：增加品檢站" /></el-form-item>
        <el-form-item label="立即生效"><el-switch v-model="dlg.activate" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlg.open = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">建立</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../../api'
import { useSession } from '../../stores/session'
import type { Station } from './types'

interface Step { seq: number; stationId: string; stationName: string; stdSecondsPerUnit: number }
interface Version { id: string; version: number; isActive: boolean; note: string | null; createdAt: string; workOrders: number; steps: Step[] }

const props = defineProps<{ productId: string; stations: Station[] }>()
const emit = defineEmits<{ changed: [] }>()
const session = useSession()
const canEdit = computed(() => session.can('routing:edit'))
const versions = ref<Version[]>([])
const saving = ref(false)
const dlg = reactive({ open: false, note: '', activate: true, steps: [] as { stationId: string; stdSecondsPerUnit: number }[] })

async function load() {
  versions.value = await api<Version[]>(`/products/${props.productId}/routings`)
}
function openNew(from?: Version) {
  dlg.steps = from ? from.steps.map((s) => ({ stationId: s.stationId, stdSecondsPerUnit: s.stdSecondsPerUnit })) : [{ stationId: '', stdSecondsPerUnit: 0 }]
  dlg.note = ''
  dlg.activate = true
  dlg.open = true
}
function move(i: number, d: number) {
  const [s] = dlg.steps.splice(i, 1)
  dlg.steps.splice(i + d, 0, s)
}
async function save() {
  if (dlg.steps.some((s) => !s.stationId)) return ElMessage.error('每一站都要選擇站點')
  saving.value = true
  try {
    const res = await api<{ version: number }>(`/products/${props.productId}/routings`, {
      method: 'POST', body: { note: dlg.note, activate: dlg.activate, steps: dlg.steps },
    })
    ElMessage.success(`已建立 v${res.version}${dlg.activate ? '並生效' : ''}`)
    dlg.open = false
    await load()
    emit('changed')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    saving.value = false
  }
}
async function activate(id: string) {
  try {
    await api(`/routings/${id}/activate`, { method: 'POST' })
    ElMessage.success('已設為生效')
    await load()
    emit('changed')
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}
onMounted(load)
</script>

<style scoped>
.bar { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 10px; flex-wrap: wrap; }
.small { font-size: 12px; }
.spacer { flex: 1; }
.ver { border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; margin-bottom: 10px; }
.ver.active { border-color: var(--status-good); }
.ver-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.flow { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-top: 8px; }
.step { display: inline-flex; flex-direction: column; padding: 4px 10px; border-radius: 6px; background: var(--surface-2); line-height: 1.3; }
.step small { color: var(--text-muted); font-size: 11px; }
.arrow { color: var(--text-muted); }
.hint { font-size: 12px; color: var(--text-muted); }
.hint.top { margin-bottom: 12px; }
.row { display: flex; align-items: center; gap: 6px; margin-bottom: 8px; flex-wrap: wrap; }
.seq { width: 28px; color: var(--text-muted); }
.st { width: 150px; }
.sec { width: 120px; }
.mt { margin-top: 14px; }
</style>
