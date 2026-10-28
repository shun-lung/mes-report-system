<template>
  <div class="login">
    <form class="card box" @submit.prevent="submit">
      <h1>報工系統</h1>
      <p class="muted">現場製造條碼報工 · 工時統計</p>
      <el-form label-position="top" @submit.prevent>
        <el-form-item label="Email">
          <el-input v-model="email" autocomplete="username" size="large" />
        </el-form-item>
        <el-form-item label="密碼">
          <el-input v-model="password" type="password" show-password autocomplete="current-password" size="large" @keyup.enter="submit" />
        </el-form-item>
        <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon class="err" />
        <el-button type="primary" size="large" :loading="loading" class="submit" @click="submit">登入</el-button>
      </el-form>
      <div v-if="isDev" class="demo">
        <div class="muted">示範帳號（點選帶入）</div>
        <button v-for="a in accounts" :key="a.email" type="button" class="acct" @click="pick(a)">
          <b>{{ a.role }}</b><span>{{ a.email }}</span>
        </button>
      </div>
    </form>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useSession } from '../stores/session'

const session = useSession()
const router = useRouter()
// 示範帳號只在本機開發模式顯示與預填；正式建置不含
const isDev = import.meta.env.DEV
const email = ref(isDev ? 'pc@demo.local' : '')
const password = ref(isDev ? 'Demo1234!' : '')
const loading = ref(false)
const error = ref('')

const accounts = !isDev ? [] : [
  { role: '最高權限', email: 'admin@example.com', password: 'admin1234' },
  { role: '生管', email: 'pc@demo.local', password: 'Demo1234!' },
  { role: '倉庫管理', email: 'wh@demo.local', password: 'Demo1234!' },
  { role: '現場組長', email: 'leader@demo.local', password: 'Demo1234!' },
  { role: '會員', email: 'member@demo.local', password: 'Demo1234!' },
]

function pick(a: (typeof accounts)[number]) {
  email.value = a.email
  password.value = a.password
}

async function submit() {
  loading.value = true
  error.value = ''
  try {
    await session.login(email.value, password.value)
    router.push('/')
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login { min-height: 100%; display: grid; place-items: center; padding: 16px; }
.box { width: 100%; max-width: 380px; padding: 28px; }
h1 { margin: 0; font-size: 24px; }
p { margin: 4px 0 20px; }
.submit { width: 100%; }
.err { margin-bottom: 12px; }
.demo { margin-top: 20px; display: grid; gap: 6px; font-size: 12px; }
.acct {
  display: flex; justify-content: space-between; gap: 8px; padding: 8px 10px; border-radius: 6px;
  border: 1px solid var(--border); background: var(--surface-2); color: var(--text-secondary); cursor: pointer; font: inherit;
}
.acct b { color: var(--text-primary); font-weight: 600; }
.acct:hover { border-color: var(--series-1); }
</style>
