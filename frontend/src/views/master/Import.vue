<template>
  <div class="page">
    <div class="page-title">
      <h2>Excel 匯入基本資料</h2>
      <span class="muted">建議順序：產品資料 → 站點與線別 → 不良代碼 → BOM → 途程</span>
    </div>

    <div class="layout">
      <div class="types" role="radiogroup" aria-label="匯入類型">
        <button v-for="(s, i) in sheets" :key="s.entity" type="button" class="type" :class="{ active: s.entity === entity }"
          role="radio" :aria-checked="s.entity === entity" @click="choose(s.entity)">
          <span class="no">{{ i + 1 }}</span>
          <span>{{ s.title }}</span>
        </button>
      </div>

      <div v-if="sheet" class="card main">
        <h3>{{ sheet.title }}</h3>
        <p class="note">{{ sheet.note }}</p>

        <ol class="steps">
          <li>
            <div class="step-title">下載範本</div>
            <div class="step-body">
              <el-button @click="getTemplate">下載「{{ sheet.title }}」範本</el-button>
              <span class="muted small">範本內有範例資料與「說明」工作表；標 * 為必填。</span>
            </div>
            <el-collapse class="cols">
              <el-collapse-item title="欄位說明" name="cols">
                <el-table :data="sheet.columns" size="small">
                  <el-table-column label="欄位" width="160">
                    <template #default="{ row }">{{ row.header }}<span v-if="row.required" class="req">*</span></template>
                  </el-table-column>
                  <el-table-column label="說明" prop="help" />
                </el-table>
              </el-collapse-item>
            </el-collapse>
          </li>
          <li>
            <div class="step-title">上傳並預覽</div>
            <div class="step-body">
              <label class="drop" :class="{ over: dragOver, busy }" @dragover.prevent="dragOver = true" @dragleave="dragOver = false" @drop.prevent="onDrop">
                <input type="file" accept=".xlsx" hidden @change="onPick" />
                <span v-if="file"><b>{{ file.name }}</b>（{{ (file.size / 1024).toFixed(0) }} KB）· 點此更換檔案</span>
                <span v-else>點此選擇或拖曳 .xlsx 檔案到這裡</span>
              </label>
              <span class="muted small">預覽只檢查、不寫入；一次最多 2,000 列。</span>
            </div>
          </li>
          <li>
            <div class="step-title">確認匯入</div>
            <div class="step-body">
              <el-button type="primary" :disabled="!canCommit" :loading="busy && committing" @click="commit">確認匯入</el-button>
              <span v-if="preview && summary.error" class="bad small">還有 {{ summary.error }} 列錯誤，請修正 Excel 後重新上傳。</span>
              <span v-else-if="preview && !changes" class="muted small">沒有需要新增或更新的資料。</span>
              <span v-else-if="preview" class="muted small">將新增 {{ summary.create }} 列、更新 {{ summary.update }} 列；整份檔案一次寫入，失敗會全部還原。</span>
            </div>
          </li>
        </ol>

        <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" class="mt" />

        <template v-if="preview">
          <div class="summary">
            <span class="chip create">＋ 新增 {{ summary.create }}</span>
            <span class="chip update">✎ 更新 {{ summary.update }}</span>
            <span class="chip same">＝ 無變更 {{ summary.same }}</span>
            <span class="chip error">✕ 錯誤 {{ summary.error }}</span>
            <span class="spacer" />
            <el-radio-group v-model="filter" size="small">
              <el-radio-button value="all">全部</el-radio-button>
              <el-radio-button value="error" :disabled="!summary.error">只看錯誤</el-radio-button>
              <el-radio-button value="change" :disabled="!changes">只看異動</el-radio-button>
            </el-radio-group>
          </div>
          <el-table :data="shown" size="small" max-height="460" :row-class-name="({ row }: any) => 'r-' + row.action">
            <el-table-column label="Excel 列" prop="row" width="80" align="right" />
            <el-table-column label="資料" prop="key" min-width="160" />
            <el-table-column label="結果" width="96">
              <template #default="{ row }"><span class="act" :class="row.action">{{ actionText[row.action] }}</span></template>
            </el-table-column>
            <el-table-column label="說明" prop="message" min-width="260" />
          </el-table>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, ApiError, download, upload } from '../../api'

interface Column { key: string; header: string; required: boolean; help: string }
interface Sheet { entity: string; title: string; note: string; columns: Column[] }
interface Result { row: number; key: string; action: 'create' | 'update' | 'same' | 'error'; message: string }
interface Preview { results: Result[]; summary: Record<string, number> }

const sheets = ref<Sheet[]>([])
const entity = ref('products')
const file = ref<File | null>(null)
const preview = ref<Preview | null>(null)
const error = ref('')
const busy = ref(false)
const committing = ref(false)
const dragOver = ref(false)
const filter = ref<'all' | 'error' | 'change'>('all')
const actionText: Record<string, string> = { create: '新增', update: '更新', same: '無變更', error: '錯誤' }

const sheet = computed(() => sheets.value.find((s) => s.entity === entity.value))
const summary = computed(() => preview.value?.summary ?? { create: 0, update: 0, same: 0, error: 0 })
const changes = computed(() => summary.value.create + summary.value.update)
const canCommit = computed(() => !!preview.value && !summary.value.error && changes.value > 0 && !busy.value)
const shown = computed(() => {
  const rows = preview.value?.results ?? []
  if (filter.value === 'error') return rows.filter((r) => r.action === 'error')
  if (filter.value === 'change') return rows.filter((r) => r.action === 'create' || r.action === 'update')
  return rows
})

function reset() {
  file.value = null
  preview.value = null
  error.value = ''
  filter.value = 'all'
}
function choose(e: string) {
  entity.value = e
  reset()
}
async function getTemplate() {
  try {
    await download(`/import/${entity.value}/template`, `${sheet.value?.title}_匯入範本.xlsx`)
  } catch (e) {
    ElMessage.error((e as Error).message)
  }
}
function onPick(e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  input.value = ''
  if (f) runPreview(f)
}
function onDrop(e: DragEvent) {
  dragOver.value = false
  const f = e.dataTransfer?.files?.[0]
  if (f) runPreview(f)
}
async function runPreview(f: File) {
  if (!f.name.toLowerCase().endsWith('.xlsx')) {
    error.value = '只支援 .xlsx 檔；若是 .xls 請在 Excel 另存新檔為 .xlsx'
    return
  }
  file.value = f
  preview.value = null
  error.value = ''
  busy.value = true
  try {
    preview.value = await upload<Preview>(`/import/${entity.value}/preview`, f)
    filter.value = preview.value.summary.error ? 'error' : 'all'
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}
async function commit() {
  if (!file.value) return
  busy.value = true
  committing.value = true
  try {
    const res = await upload<{ summary: Record<string, number> }>(`/import/${entity.value}/commit`, file.value)
    ElMessage.success(`匯入完成：新增 ${res.summary.create} 列、更新 ${res.summary.update} 列`)
    reset()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : (e as Error).message
  } finally {
    busy.value = false
    committing.value = false
  }
}

onMounted(async () => {
  sheets.value = await api<Sheet[]>('/import/sheets')
})
</script>

<style scoped>
.layout { display: grid; grid-template-columns: 200px minmax(0, 1fr); gap: 16px; align-items: start; }
.types { display: flex; flex-direction: column; gap: 6px; }
.type {
  display: flex; align-items: center; gap: 10px; padding: 12px 14px; border-radius: 10px; border: 1px solid var(--border);
  background: var(--surface-1); color: var(--text-primary); font: inherit; text-align: left; cursor: pointer;
}
.type:hover { border-color: var(--series-1); }
.type.active { border-color: var(--series-1); background: var(--series-1-soft); font-weight: 600; }
.no { width: 22px; height: 22px; border-radius: 50%; display: grid; place-items: center; font-size: 12px; background: var(--surface-2); color: var(--text-secondary); flex: none; }
.type.active .no { background: var(--series-1); color: #fff; }
.note { margin: 0 0 12px; color: var(--text-secondary); font-size: 13px; line-height: 1.6; }
.steps { margin: 0; padding-left: 20px; display: flex; flex-direction: column; gap: 14px; }
.step-title { font-weight: 600; margin-bottom: 6px; }
.step-body { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.small { font-size: 12px; }
.req { color: var(--div-neg-ink); margin-left: 2px; }
.cols { margin-top: 8px; border: 0; }
.drop {
  display: inline-flex; align-items: center; min-width: min(420px, 100%); padding: 16px 18px; border: 2px dashed var(--border);
  border-radius: 10px; cursor: pointer; color: var(--text-secondary); background: var(--surface-0);
}
.drop:hover, .drop.over { border-color: var(--series-1); color: var(--text-primary); }
.drop.busy { opacity: 0.6; pointer-events: none; }
.bad { color: var(--div-neg-ink); }
.mt { margin-top: 12px; }
.summary { display: flex; align-items: center; gap: 8px; margin: 16px 0 8px; flex-wrap: wrap; }
.spacer { flex: 1; }
.chip { padding: 4px 10px; border-radius: 999px; font-size: 13px; background: var(--surface-2); color: var(--text-primary); }
.chip.create { background: var(--div-pos-1); }
.chip.update { background: var(--div-pos-1); }
.chip.error { background: var(--div-neg-1); }
.act { font-weight: 600; }
.act.create, .act.update { color: var(--div-pos-ink); }
.act.same { color: var(--text-muted); font-weight: 400; }
.act.error { color: var(--div-neg-ink); }
:deep(.r-error) td { background: var(--div-neg-1) !important; }
@media (max-width: 760px) {
  .layout { grid-template-columns: 1fr; }
  .types { flex-direction: row; flex-wrap: wrap; }
}
</style>
