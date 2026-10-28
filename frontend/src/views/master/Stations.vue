<template>
  <div class="page">
    <div class="page-title">
      <h2>站點與線別</h2>
      <span class="muted">時產能與標準人數用於看板目標與之後的排程</span>
      <div class="spacer" />
      <el-button v-if="canEdit" type="primary" @click="editStation()">新增站點</el-button>
    </div>

    <div v-for="s in stations" :key="s.id" class="card">
      <h3>
        <span class="seq num">{{ s.sortOrder }}</span>{{ s.name }}
        <small>{{ s.code }} · {{ s.lines.length }} 條線</small>
        <el-tag v-if="!s.isActive" size="small" type="info">停用</el-tag>
        <span class="spacer" />
        <template v-if="canEdit">
          <el-button size="small" text @click="editStation(s)">編輯站點</el-button>
          <el-button size="small" @click="editLine(s)">新增線別</el-button>
        </template>
      </h3>
      <el-table :data="s.lines" size="small" empty-text="尚無線別">
        <el-table-column label="代碼" prop="code" width="120" />
        <el-table-column label="名稱" prop="name" min-width="140" />
        <el-table-column label="時產能（雙）" width="120" align="right">
          <template #default="{ row }"><span class="num">{{ row.capacityPerHour }}</span></template>
        </el-table-column>
        <el-table-column label="標準人數" prop="stdHeadcount" width="100" align="right" />
        <el-table-column label="狀態" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.isActive ? 'success' : 'info'">{{ row.isActive ? '使用中' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column v-if="canEdit" width="80">
          <template #default="{ row }"><el-button size="small" text @click="editLine(s, row)">編輯</el-button></template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="stationDlg.open" :title="stationDlg.id ? '編輯站點' : '新增站點'" width="min(440px, 94vw)">
      <el-form label-width="80px" @submit.prevent>
        <el-form-item label="代碼" required><el-input v-model="stationDlg.form.code" maxlength="20" /></el-form-item>
        <el-form-item label="名稱" required><el-input v-model="stationDlg.form.name" maxlength="100" /></el-form-item>
        <el-form-item label="順序"><el-input-number v-model="stationDlg.form.sortOrder" :min="0" :step="10" /></el-form-item>
        <el-form-item label="使用中"><el-switch v-model="stationDlg.form.isActive" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="stationDlg.open = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveStation">儲存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="lineDlg.open" :title="lineDlg.id ? '編輯線別' : '新增線別'" width="min(440px, 94vw)">
      <el-form label-width="90px" @submit.prevent>
        <el-form-item label="站點">
          <el-select v-model="lineDlg.form.stationId">
            <el-option v-for="s in stations" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="代碼" required><el-input v-model="lineDlg.form.code" maxlength="20" /></el-form-item>
        <el-form-item label="名稱" required><el-input v-model="lineDlg.form.name" maxlength="100" /></el-form-item>
        <el-form-item label="時產能"><el-input-number v-model="lineDlg.form.capacityPerHour" :min="0" :step="10" /> <span class="unit">雙／小時</span></el-form-item>
        <el-form-item label="標準人數"><el-input-number v-model="lineDlg.form.stdHeadcount" :min="0" /></el-form-item>
        <el-form-item label="使用中"><el-switch v-model="lineDlg.form.isActive" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="lineDlg.open = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveLine">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../../api'
import { useSession } from '../../stores/session'

interface Line { id: string; stationId: string; code: string; name: string; capacityPerHour: number; stdHeadcount: number; isActive: boolean }
interface Station { id: string; code: string; name: string; sortOrder: number; isActive: boolean; lines: Line[] }

const session = useSession()
const canEdit = computed(() => session.can('station:edit'))
const stations = ref<Station[]>([])
const saving = ref(false)
const stationDlg = reactive({ open: false, id: '', form: { code: '', name: '', sortOrder: 0, isActive: true } })
const lineDlg = reactive({
  open: false, id: '',
  form: { stationId: '', code: '', name: '', capacityPerHour: 60, stdHeadcount: 0, isActive: true },
})

async function load() {
  stations.value = await api<Station[]>('/stations')
}
function editStation(s?: Station) {
  stationDlg.id = s?.id ?? ''
  const next = (stations.value.at(-1)?.sortOrder ?? 0) + 10
  stationDlg.form = s ? { code: s.code, name: s.name, sortOrder: s.sortOrder, isActive: s.isActive } : { code: '', name: '', sortOrder: next, isActive: true }
  stationDlg.open = true
}
function editLine(s: Station, l?: Line) {
  lineDlg.id = l?.id ?? ''
  lineDlg.form = l
    ? { stationId: l.stationId, code: l.code, name: l.name, capacityPerHour: l.capacityPerHour, stdHeadcount: l.stdHeadcount, isActive: l.isActive }
    : { stationId: s.id, code: `${s.code}-`, name: `${s.name} `, capacityPerHour: 60, stdHeadcount: 0, isActive: true }
  lineDlg.open = true
}
async function save(fn: () => Promise<unknown>, close: () => void) {
  saving.value = true
  try {
    await fn()
    ElMessage.success('已儲存')
    close()
    await load()
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    saving.value = false
  }
}
const saveStation = () =>
  save(
    () => api(stationDlg.id ? `/stations/${stationDlg.id}` : '/stations', { method: stationDlg.id ? 'PUT' : 'POST', body: stationDlg.form }),
    () => (stationDlg.open = false),
  )
const saveLine = () =>
  save(
    () => api(lineDlg.id ? `/lines/${lineDlg.id}` : '/lines', { method: lineDlg.id ? 'PUT' : 'POST', body: lineDlg.form }),
    () => (lineDlg.open = false),
  )
onMounted(load)
</script>

<style scoped>
.spacer { flex: 1; }
.card h3 { align-items: center; }
.seq { display: inline-block; min-width: 28px; color: var(--text-muted); font-weight: 400; }
.unit { margin-left: 8px; color: var(--text-muted); }
</style>
