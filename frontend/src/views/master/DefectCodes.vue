<template>
  <div class="page">
    <div class="page-title">
      <h2>不良代碼</h2>
      <span class="muted">現場回報不良時選用；未指定站點為全站通用</span>
      <div class="spacer" />
      <el-button v-if="canEdit" type="primary" @click="edit()">新增不良代碼</el-button>
    </div>
    <div class="card">
      <el-table :data="codes" size="small">
        <el-table-column label="站點" width="130">
          <template #default="{ row }">{{ row.stationName ?? '全站通用' }}</template>
        </el-table-column>
        <el-table-column label="代碼" prop="code" width="100" />
        <el-table-column label="名稱" prop="name" min-width="160" />
        <el-table-column label="分類" prop="category" width="120" />
        <el-table-column label="狀態" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.isActive ? 'success' : 'info'">{{ row.isActive ? '使用中' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column v-if="canEdit" width="80">
          <template #default="{ row }"><el-button size="small" text @click="edit(row)">編輯</el-button></template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="dlg.open" :title="dlg.id ? '編輯不良代碼' : '新增不良代碼'" width="min(440px, 94vw)">
      <el-form label-width="80px" @submit.prevent>
        <el-form-item label="站點">
          <el-select v-model="dlg.form.stationId" clearable placeholder="全站通用">
            <el-option v-for="s in stations" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="代碼" required><el-input v-model="dlg.form.code" maxlength="20" /></el-form-item>
        <el-form-item label="名稱" required><el-input v-model="dlg.form.name" maxlength="100" /></el-form-item>
        <el-form-item label="分類"><el-input v-model="dlg.form.category" maxlength="50" placeholder="例如：外觀、尺寸、車縫" /></el-form-item>
        <el-form-item label="使用中"><el-switch v-model="dlg.form.isActive" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlg.open = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../../api'
import { useSession } from '../../stores/session'

interface Code { id: string; stationId: string | null; stationName: string | null; code: string; name: string; category: string | null; isActive: boolean }

const session = useSession()
const canEdit = computed(() => session.can('defect:edit'))
const codes = ref<Code[]>([])
const stations = ref<{ id: string; name: string }[]>([])
const saving = ref(false)
const dlg = reactive({ open: false, id: '', form: { stationId: null as string | null, code: '', name: '', category: '', isActive: true } })

async function load() {
  ;[codes.value, stations.value] = await Promise.all([api<Code[]>('/defect-codes'), api<any[]>('/stations')])
}
function edit(c?: Code) {
  dlg.id = c?.id ?? ''
  dlg.form = c
    ? { stationId: c.stationId, code: c.code, name: c.name, category: c.category ?? '', isActive: c.isActive }
    : { stationId: null, code: '', name: '', category: '', isActive: true }
  dlg.open = true
}
async function save() {
  saving.value = true
  try {
    const body = { ...dlg.form, stationId: dlg.form.stationId || null }
    await api(dlg.id ? `/defect-codes/${dlg.id}` : '/defect-codes', { method: dlg.id ? 'PUT' : 'POST', body })
    ElMessage.success('已儲存')
    dlg.open = false
    await load()
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    saving.value = false
  }
}
onMounted(load)
</script>

<style scoped>
.spacer { flex: 1; }
</style>
