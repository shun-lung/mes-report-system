<template>
  <div class="page">
    <div class="page-title"><h2>權限管理</h2><span class="muted">{{ session.factory?.code }} {{ session.factory?.name }}</span></div>
    <div class="card">
      <h3>使用者</h3>
      <el-table :data="users" size="small">
        <el-table-column label="Email" prop="email" min-width="200" />
        <el-table-column label="姓名" prop="name" width="120" />
        <el-table-column label="工號" prop="employeeNo" width="90" />
        <el-table-column label="角色" min-width="180">
          <template #default="{ row }">
            <el-tag v-if="row.isSuperAdmin" type="danger" size="small" class="ml">總部管理員</el-tag>
            <el-tag v-for="r in row.roles" :key="r" size="small" class="ml">{{ r }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="授權線別（生管管制用）" min-width="220">
          <template #default="{ row }">
            <span v-if="row.isSuperAdmin" class="muted small">不受限制</span>
            <template v-else>
              <span v-if="!row.lines.length" class="muted small">未設定</span>
              <span v-else class="small">{{ row.lines.join('、') }}</span>
              <el-button v-if="canEditUsers" size="small" text @click="openLines(row)">設定</el-button>
            </template>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <el-dialog v-model="lineDlg.open" :title="`授權線別：${lineDlg.name}`" width="min(560px, 94vw)">
      <p class="muted small">製令設為「人員授權」時，只有被授權該線別的人能在那條線回報；掃描畫面也只會列出授權的線別。</p>
      <div v-for="st in stations" :key="st.id" class="st-group">
        <el-checkbox :model-value="st.lines.every((l) => lineDlg.ids.includes(l.id))"
          :indeterminate="st.lines.some((l) => lineDlg.ids.includes(l.id)) && !st.lines.every((l) => lineDlg.ids.includes(l.id))"
          @change="(v: any) => toggleStation(st, !!v)"><b>{{ st.name }}</b></el-checkbox>
        <el-checkbox-group v-model="lineDlg.ids" class="lines">
          <el-checkbox v-for="l in st.lines" :key="l.id" :value="l.id">{{ l.code }}</el-checkbox>
        </el-checkbox-group>
      </div>
      <template #footer>
        <el-button @click="lineDlg.ids = []">全部清除</el-button>
        <el-button @click="lineDlg.open = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveLines">儲存</el-button>
      </template>
    </el-dialog>

    <div class="card">
      <h3>角色 × 權限 <small>✓ = 具備</small></h3>
      <div class="matrix-wrap">
        <table class="matrix">
          <thead>
            <tr>
              <th class="left">模組</th>
              <th class="left">權限</th>
              <th v-for="r in roles" :key="r.id">{{ r.name }}<div class="muted small">{{ r.userCount }} 人</div></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in perms" :key="p.code">
              <td class="left muted">{{ moduleName[p.module] ?? p.module }}</td>
              <td class="left">{{ p.name }} <span class="muted small">{{ p.code }}</span></td>
              <td v-for="r in roles" :key="r.id" class="mark">{{ r.permissions.includes(p.code) ? '✓' : '' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../api'
import { useSession } from '../stores/session'

const session = useSession()
const users = ref<any[]>([])
const roles = ref<{ id: string; name: string; permissions: string[]; userCount: number }[]>([])
const perms = ref<{ code: string; module: string; name: string }[]>([])
const moduleName: Record<string, string> = {
  system: '系統', org: '組織', masterdata: '基本資料', production: '生產', inventory: '倉儲', report: '報表',
}
const canEditUsers = computed(() => session.can('user:edit'))
const stations = ref<{ id: string; name: string; lines: { id: string; code: string; isActive: boolean }[] }[]>([])
const lineDlg = reactive({ open: false, userId: '', name: '', ids: [] as string[] })
const saving = ref(false)

async function load() {
  ;[users.value, roles.value, perms.value] = await Promise.all([api<any[]>('/users'), api<any[]>('/roles'), api<any[]>('/permissions')])
}
async function openLines(u: any) {
  if (!stations.value.length) {
    stations.value = (await api<any[]>('/stations')).map((s) => ({ ...s, lines: s.lines.filter((l: any) => l.isActive) })).filter((s) => s.lines.length)
  }
  Object.assign(lineDlg, { open: true, userId: u.id, name: u.name, ids: await api<string[]>(`/users/${u.id}/lines`) })
}
function toggleStation(st: { lines: { id: string }[] }, on: boolean) {
  const ids = new Set(lineDlg.ids)
  for (const l of st.lines) on ? ids.add(l.id) : ids.delete(l.id)
  lineDlg.ids = [...ids]
}
async function saveLines() {
  saving.value = true
  try {
    await api(`/users/${lineDlg.userId}/lines`, { method: 'PUT', body: { lineIds: lineDlg.ids } })
    ElMessage.success('已儲存授權線別')
    lineDlg.open = false
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
.ml { margin-right: 4px; }
.small { font-size: 11px; }
.matrix-wrap { overflow-x: auto; }
.matrix { border-collapse: collapse; width: 100%; font-size: 13px; }
.matrix th, .matrix td { padding: 6px 10px; border-bottom: 1px solid var(--grid); text-align: center; white-space: nowrap; }
.matrix th { font-weight: 600; }
.left { text-align: left !important; }
.mark { color: var(--series-1); font-weight: 700; }
.st-group { padding: 8px 0; border-bottom: 1px solid var(--grid); }
.lines { margin-left: 24px; display: flex; flex-wrap: wrap; }
</style>
