<template>
  <div class="page">
    <div class="page-title"><h2>權限管理</h2><span class="muted">{{ session.factory?.code }} {{ session.factory?.name }}</span></div>
    <div class="card">
      <div class="head">
        <h3>使用者</h3>
        <el-button v-if="canEditUsers" type="primary" @click="openUser()">新增使用者</el-button>
      </div>
      <el-table :data="users" size="small">
        <el-table-column label="Email（帳號）" prop="email" min-width="200" />
        <el-table-column label="姓名" prop="name" width="110" />
        <el-table-column label="工號" prop="employeeNo" width="80" />
        <el-table-column label="角色" min-width="160">
          <template #default="{ row }">
            <el-tag v-if="row.isSuperAdmin" type="danger" size="small" class="ml">總部管理員</el-tag>
            <el-tag v-for="r in row.roles" :key="r" size="small" class="ml">{{ r }}</el-tag>
            <el-tag v-if="!row.isActive" type="info" size="small" class="ml">已停用</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="授權線別（生管管制用）" min-width="200">
          <template #default="{ row }">
            <span v-if="row.isSuperAdmin" class="muted small">不受限制</span>
            <template v-else>
              <span v-if="!row.lines.length" class="muted small">未設定</span>
              <span v-else class="small">{{ row.lines.join('、') }}</span>
              <el-button v-if="canEditUsers" size="small" text @click="openLines(row)">設定</el-button>
            </template>
          </template>
        </el-table-column>
        <el-table-column v-if="canEditUsers" label="操作" width="80">
          <template #default="{ row }">
            <el-button v-if="!row.isSuperAdmin" size="small" text type="primary" @click="openUser(row)">編輯</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="userDlg.open" :title="userDlg.id ? `編輯使用者：${userDlg.form.name}` : '新增使用者'" width="min(520px, 94vw)">
      <el-form label-width="96px" @submit.prevent>
        <el-form-item label="Email" required>
          <el-input v-model="userDlg.form.email" :disabled="!!userDlg.id" placeholder="登入帳號，例如 wang@company.com" />
        </el-form-item>
        <el-form-item label="姓名" required><el-input v-model="userDlg.form.name" maxlength="100" /></el-form-item>
        <el-form-item label="工號"><el-input v-model="userDlg.form.employeeNo" maxlength="30" /></el-form-item>
        <el-form-item :label="userDlg.id ? '重設密碼' : '密碼'" :required="!userDlg.id">
          <el-input v-model="userDlg.form.password" type="password" show-password autocomplete="new-password"
            :placeholder="userDlg.id ? '不修改請留空' : '至少 8 碼'" />
        </el-form-item>
        <el-form-item label="角色" required>
          <div v-if="userDlg.self" class="muted small">不能修改自己的角色，請由其他管理員調整。</div>
          <el-checkbox-group v-else v-model="userDlg.form.roleIds">
            <el-checkbox v-for="r in roles.filter((x) => x.editable || x.isSystem)" :key="r.id" :value="r.id"
              :disabled="!grantable(r)">{{ r.name }}</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item v-if="userDlg.id" label="帳號狀態">
          <el-switch v-model="userDlg.form.isActive" :disabled="userDlg.self" active-text="啟用" inactive-text="停用" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="userDlg.open = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveUser">儲存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="lineDlg.open" :title="`授權線別：${lineDlg.name}`" width="min(560px, 94vw)">
      <p class="muted small">製令設為「人員授權」時，只有被授權該線別的人能在那條線回報；掃描畫面也只會列出授權的線別。</p>
      <div v-for="st in stations" :key="st.id" class="st-group">
        <el-checkbox :model-value="st.lines.every((l) => lineDlg.ids.includes(l.id))"
          :indeterminate="st.lines.some((l) => lineDlg.ids.includes(l.id)) && !st.lines.every((l) => lineDlg.ids.includes(l.id))"
          @change="(v: any) => toggleStation(st, !!v)"><b>{{ st.name }}</b></el-checkbox>
        <el-checkbox-group v-model="lineDlg.ids" class="lines">
          <el-checkbox v-for="l in st.lines" :key="l.id" :value="l.id">{{ l.name }}</el-checkbox>
        </el-checkbox-group>
      </div>
      <template #footer>
        <el-button @click="lineDlg.ids = []">全部清除</el-button>
        <el-button @click="lineDlg.open = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveLines">儲存</el-button>
      </template>
    </el-dialog>

    <div class="card">
      <div class="head">
        <h3>角色 × 權限 <small>✓ = 具備</small></h3>
        <el-button v-if="canEditRoles" type="primary" @click="openRole()">新增角色</el-button>
      </div>
      <div class="matrix-wrap">
        <table class="matrix">
          <thead>
            <tr>
              <th class="left">模組</th>
              <th class="left">權限</th>
              <th v-for="r in roles" :key="r.id">
                {{ r.name }}<div class="muted small">{{ r.userCount }} 人</div>
                <div v-if="canEditRoles && r.editable" class="role-ops">
                  <el-button size="small" text type="primary" @click="openRole(r)">編輯</el-button>
                  <el-button size="small" text type="primary" @click="openRole(r, true)">複製</el-button>
                  <el-button size="small" text type="danger" @click="removeRole(r)">刪除</el-button>
                </div>
                <div v-else-if="r.isSystem" class="muted small">系統固定</div>
              </th>
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

    <el-dialog v-model="roleDlg.open" :title="roleDlg.id ? `編輯角色：${roleDlg.form.name}` : '新增角色'" width="min(640px, 94vw)">
      <el-form label-width="80px" @submit.prevent>
        <el-form-item label="代碼" required>
          <el-input v-model="roleDlg.form.code" :disabled="!!roleDlg.id" maxlength="50" placeholder="英文代碼，例如 QC" />
        </el-form-item>
        <el-form-item label="名稱" required><el-input v-model="roleDlg.form.name" maxlength="100" placeholder="例如 品管" /></el-form-item>
      </el-form>
      <p class="muted small">勾選此角色可以使用的功能。灰色選項是你自己沒有的權限，不能授予。</p>
      <div v-for="g in permGroups" :key="g.module" class="st-group">
        <el-checkbox :model-value="g.perms.every((p) => roleDlg.form.permissions.includes(p.code))"
          :indeterminate="g.perms.some((p) => roleDlg.form.permissions.includes(p.code)) && !g.perms.every((p) => roleDlg.form.permissions.includes(p.code))"
          @change="(v: any) => toggleModule(g.perms, !!v)"><b>{{ moduleName[g.module] ?? g.module }}</b></el-checkbox>
        <el-checkbox-group v-model="roleDlg.form.permissions" class="lines">
          <el-checkbox v-for="p in g.perms" :key="p.code" :value="p.code"
            :disabled="!session.can(p.code) && !roleDlg.form.permissions.includes(p.code)">{{ p.name }}</el-checkbox>
        </el-checkbox-group>
      </div>
      <template #footer>
        <span class="muted small count">已選 {{ roleDlg.form.permissions.length }} 項</span>
        <el-button @click="roleDlg.open = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveRole">儲存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '../api'
import { useSession } from '../stores/session'

interface Role { id: string; code: string; name: string; permissions: string[]; userCount: number; isSystem: boolean; editable: boolean }
interface Perm { code: string; module: string; name: string }

const session = useSession()
const users = ref<any[]>([])
const roles = ref<Role[]>([])
const perms = ref<Perm[]>([])
const moduleName: Record<string, string> = {
  system: '系統', org: '組織', masterdata: '基本資料', production: '生產', inventory: '倉儲', purchase: '採購', report: '報表',
}
const canEditUsers = computed(() => session.can('user:edit'))
const canEditRoles = computed(() => session.can('role:edit'))
const permGroups = computed(() => {
  const groups: { module: string; perms: Perm[] }[] = []
  for (const p of perms.value) {
    const g = groups.find((x) => x.module === p.module)
    g ? g.perms.push(p) : groups.push({ module: p.module, perms: [p] })
  }
  return groups
})
const stations = ref<{ id: string; name: string; lines: { id: string; code: string; name: string; isActive: boolean }[] }[]>([])
const lineDlg = reactive({ open: false, userId: '', name: '', ids: [] as string[] })
const userDlg = reactive({
  open: false, id: '', self: false,
  form: { email: '', name: '', employeeNo: '', password: '', roleIds: [] as string[], isActive: true },
})
const roleDlg = reactive({ open: false, id: '', form: { code: '', name: '', permissions: [] as string[] } })
const saving = ref(false)

async function load() {
  ;[users.value, roles.value, perms.value] = await Promise.all([api<any[]>('/users'), api<Role[]>('/roles'), api<Perm[]>('/permissions')])
}
async function run(fn: () => Promise<unknown>, ok: string) {
  saving.value = true
  try {
    await fn()
    ElMessage.success(ok)
    await load()
    return true
  } catch (e) {
    ElMessage.error((e as Error).message)
    return false
  } finally {
    saving.value = false
  }
}

// 只能指派權限不超過自己的角色（與後端規則一致）
const grantable = (r: Role) => r.permissions.every((p) => session.can(p))

function openUser(u?: any) {
  userDlg.id = u?.id ?? ''
  userDlg.self = !!u && u.id === session.user?.id
  userDlg.form = {
    email: u?.email ?? '', name: u?.name ?? '', employeeNo: u?.employeeNo ?? '', password: '',
    roleIds: [...(u?.roleIds ?? [])], isActive: u?.isActive ?? true,
  }
  userDlg.open = true
}
async function saveUser() {
  const f = userDlg.form
  if (!f.name.trim() || (!userDlg.id && !f.email.trim())) return ElMessage.warning('請輸入 Email 與姓名')
  if (!userDlg.id && f.password.length < 8) return ElMessage.warning('密碼至少 8 碼')
  if (!userDlg.self && !f.roleIds.length) return ElMessage.warning('請至少勾選一個角色')
  const done = await run(
    () => userDlg.id
      ? api(`/users/${userDlg.id}`, { method: 'PUT', body: f })
      : api('/users', { method: 'POST', body: f }),
    userDlg.id ? '已更新使用者' : `已建立帳號 ${f.email}`,
  )
  if (done) userDlg.open = false
}

function openRole(r?: Role, copy = false) {
  roleDlg.id = r && !copy ? r.id : ''
  roleDlg.form = {
    code: r && !copy ? r.code : '',
    name: r ? (copy ? r.name + '（複製）' : r.name) : '',
    permissions: [...(r?.permissions ?? [])].filter((p) => !copy || session.can(p)),
  }
  roleDlg.open = true
}
function toggleModule(list: Perm[], on: boolean) {
  const set = new Set(roleDlg.form.permissions)
  for (const p of list) {
    if (on && session.can(p.code)) set.add(p.code)
    else if (!on) set.delete(p.code)
  }
  roleDlg.form.permissions = [...set]
}
async function saveRole() {
  const f = roleDlg.form
  if (!f.code.trim() || !f.name.trim()) return ElMessage.warning('請輸入角色代碼與名稱')
  const done = await run(
    () => roleDlg.id
      ? api(`/roles/${roleDlg.id}`, { method: 'PUT', body: f })
      : api('/roles', { method: 'POST', body: f }),
    roleDlg.id ? '已更新角色權限' : `已新增角色「${f.name}」`,
  )
  if (done) {
    roleDlg.open = false
    await session.loadMe() // 自己所屬的角色若被修改，選單立即更新
  }
}
async function removeRole(r: Role) {
  if (r.userCount) return ElMessage.warning(`還有 ${r.userCount} 位使用者是「${r.name}」，請先改派其他角色`)
  try {
    await ElMessageBox.confirm(`確定刪除角色「${r.name}」？`, '刪除角色', { type: 'warning' })
  } catch {
    return
  }
  await run(() => api(`/roles/${r.id}`, { method: 'DELETE' }), '已刪除角色')
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
  const done = await run(() => api(`/users/${lineDlg.userId}/lines`, { method: 'PUT', body: { lineIds: lineDlg.ids } }), '已儲存授權線別')
  if (done) lineDlg.open = false
}
onMounted(load)
</script>

<style scoped>
.ml { margin-right: 4px; }
.small { font-size: 11px; }
.head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.head h3 { margin: 0; }
.matrix-wrap { overflow-x: auto; }
.matrix { border-collapse: collapse; width: 100%; font-size: 13px; }
.matrix th, .matrix td { padding: 6px 10px; border-bottom: 1px solid var(--grid); text-align: center; white-space: nowrap; }
.matrix th { font-weight: 600; vertical-align: top; }
.role-ops { display: flex; justify-content: center; }
.role-ops .el-button { padding: 0 3px; margin: 0; }
.left { text-align: left !important; }
.mark { color: var(--series-1); font-weight: 700; }
.st-group { padding: 8px 0; border-bottom: 1px solid var(--grid); }
.lines { margin-left: 24px; display: flex; flex-wrap: wrap; }
.count { margin-right: 12px; }
</style>
