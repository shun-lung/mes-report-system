<template>
  <div class="shell">
    <header class="top">
      <button class="menu-btn" type="button" aria-label="選單" @click="navOpen = !navOpen">☰</button>
      <div class="brand">報工系統</div>
      <el-select v-if="session.factories.length > 1" :model-value="session.factoryId" size="small" class="factory"
        @update:model-value="switchFactory">
        <el-option v-for="f in session.factories" :key="f.id" :label="`${f.code} ${f.name}`" :value="f.id" />
      </el-select>
      <span v-else-if="session.factory" class="factory-name">{{ session.factory.code }} {{ session.factory.name }}</span>
      <div class="spacer" />
      <button v-if="session.can('scan:report')" type="button" class="scan-link" @click="scanOpen = true">現場掃描</button>
      <el-badge :value="pending.length" :hidden="!pending.length" class="bell">
        <el-button size="small" text @click="alertOpen = true">缺料預警</el-button>
      </el-badge>
      <span class="user">{{ session.user?.name }}</span>
      <el-button size="small" text @click="logout">登出</el-button>
    </header>
    <div class="body">
      <nav :class="{ open: navOpen }" @click="navOpen = false">
        <template v-for="(m, i) in menu" :key="m.path">
          <div v-if="m.group && m.group !== menu[i - 1]?.group" class="nav-group">{{ m.group }}</div>
          <router-link :to="m.path" class="nav-item" active-class="" exact-active-class="active">{{ m.title }}</router-link>
        </template>
      </nav>
      <main>
        <el-empty v-if="!session.factory" description="此帳號尚未被指派任何工廠" />
        <router-view v-else-if="allowed" :key="session.factoryId" />
        <el-result v-else icon="warning" title="權限不足" sub-title="請聯絡管理員開通此頁面權限" />
      </main>
    </div>

    <el-dialog v-model="scanOpen" :fullscreen="scanFull" width="min(1240px, 98vw)" top="2vh" destroy-on-close
      class="scan-dialog" :show-close="false" @closed="exitFullscreen">
      <template #header>
        <div class="scan-head">
          <b>現場掃描</b>
          <span class="spacer" />
          <el-button size="small" @click="toggleFull">{{ scanFull ? '離開全螢幕' : '全螢幕' }}</el-button>
          <el-button size="small" @click="scanOpen = false">關閉</el-button>
        </div>
      </template>
      <StationPanel embedded />
    </el-dialog>

    <el-dialog v-model="alertOpen" title="缺料預警 — 請選擇「通知建單」或「Pass」" width="min(920px, 96vw)">
      <AlertList :alerts="pending" @decided="loadAlerts" />
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import { useSession } from '../stores/session'
import AlertList, { type Alert } from './AlertList.vue'
import StationPanel from './station/StationPanel.vue'

const session = useSession()
const route = useRoute()
const router = useRouter()
const navOpen = ref(false)
const alertOpen = ref(false)
const pending = ref<Alert[]>([])
const scanOpen = ref(false)
const scanFull = ref(false)

// 全螢幕：對話框鋪滿，並在支援的裝置（平板）切換瀏覽器全螢幕
async function toggleFull() {
  scanFull.value = !scanFull.value
  try {
    if (scanFull.value && !document.fullscreenElement) await document.documentElement.requestFullscreen()
    else if (!scanFull.value && document.fullscreenElement) await document.exitFullscreen()
  } catch {
    /* 不支援全螢幕時只鋪滿對話框 */
  }
}
function exitFullscreen() {
  scanFull.value = false
  if (document.fullscreenElement) document.exitFullscreen().catch(() => {})
}

const menu = computed(() =>
  router.options.routes
    .find((r) => r.path === '/')!
    .children!.filter((c) => !c.meta?.perm || session.can(c.meta.perm as string))
    .map((c) => ({ path: '/' + c.path, title: c.meta!.title as string, group: c.meta!.group as string | undefined })),
)
const allowed = computed(() => !route.meta.perm || session.can(route.meta.perm as string))

async function loadAlerts() {
  if (!session.factory) return
  pending.value = await api<Alert[]>('/alerts?status=PENDING').catch(() => [])
  if (!pending.value.length) alertOpen.value = false
}

// 登入或切換工廠後，有決策權限者若有待處理預警即彈窗
async function checkOnEnter() {
  await loadAlerts()
  if (pending.value.length && session.can('alert:decide')) alertOpen.value = true
}

function switchFactory(id: string) {
  session.selectFactory(id)
}

function logout() {
  session.logout()
  router.push('/login')
}

onMounted(() => {
  checkOnEnter()
  window.addEventListener('mes:alerts-changed', checkOnEnter)
})
onUnmounted(() => window.removeEventListener('mes:alerts-changed', checkOnEnter))
watch(() => session.factoryId, checkOnEnter)
</script>

<style scoped>
.shell { min-height: 100%; display: flex; flex-direction: column; }
.top {
  height: 52px; display: flex; align-items: center; gap: 12px; padding: 0 16px;
  background: var(--surface-1); border-bottom: 1px solid var(--border); position: sticky; top: 0; z-index: 20;
}
.brand { font-weight: 700; font-size: 16px; }
.factory { width: 180px; }
.factory-name { color: var(--text-secondary); }
.spacer { flex: 1; }
.user { color: var(--text-secondary); }
.scan-link { padding: 5px 12px; border-radius: 6px; background: var(--series-1); color: #fff; text-decoration: none; font-size: 13px; border: 0; cursor: pointer; font: inherit; }
.scan-head { display: flex; align-items: center; gap: 8px; }
.scan-head .spacer { flex: 1; }
:global(.scan-dialog .el-dialog__body) { padding: 0; }
:global(.scan-dialog .el-dialog__header) { padding-bottom: 8px; }
.menu-btn { display: none; background: none; border: 0; font-size: 18px; color: var(--text-primary); cursor: pointer; }
.body { flex: 1; display: flex; min-height: 0; }
nav {
  width: 168px; flex: none; padding: 12px 8px; background: var(--surface-1); border-right: 1px solid var(--border);
  display: flex; flex-direction: column; gap: 2px;
}
.nav-group { padding: 14px 12px 4px; font-size: 11px; color: var(--text-muted); letter-spacing: 0.5px; }
.nav-item { padding: 9px 12px; border-radius: 6px; color: var(--text-secondary); text-decoration: none; }
.nav-item:hover { background: var(--surface-2); color: var(--text-primary); }
.nav-item.active { background: var(--series-1-soft); color: var(--text-primary); font-weight: 600; }
main { flex: 1; min-width: 0; overflow-x: hidden; }
@media (max-width: 760px) {
  .menu-btn { display: block; }
  .user { display: none; }
  nav { position: fixed; top: 52px; bottom: 0; left: 0; z-index: 30; transform: translateX(-100%); transition: transform 0.15s; }
  nav.open { transform: none; box-shadow: 4px 0 20px rgb(0 0 0 / 0.15); }
}
</style>
