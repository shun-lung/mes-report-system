<template>
  <div class="page">
    <div class="page-title"><h2>權限管理</h2><span class="muted">{{ session.factory?.code }} {{ session.factory?.name }}</span></div>
    <div class="card">
      <h3>使用者</h3>
      <el-table :data="users" size="small">
        <el-table-column label="Email" prop="email" min-width="200" />
        <el-table-column label="姓名" prop="name" width="120" />
        <el-table-column label="工號" prop="employeeNo" width="90" />
        <el-table-column label="角色" min-width="200">
          <template #default="{ row }">
            <el-tag v-if="row.isSuperAdmin" type="danger" size="small" class="ml">總部管理員</el-tag>
            <el-tag v-for="r in row.roles" :key="r" size="small" class="ml">{{ r }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
    </div>
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
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { useSession } from '../stores/session'

const session = useSession()
const users = ref<any[]>([])
const roles = ref<{ id: string; name: string; permissions: string[]; userCount: number }[]>([])
const perms = ref<{ code: string; module: string; name: string }[]>([])
const moduleName: Record<string, string> = {
  system: '系統', org: '組織', masterdata: '基本資料', production: '生產', inventory: '倉儲', report: '報表',
}
onMounted(async () => {
  ;[users.value, roles.value, perms.value] = await Promise.all([api<any[]>('/users'), api<any[]>('/roles'), api<any[]>('/permissions')])
})
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
</style>
