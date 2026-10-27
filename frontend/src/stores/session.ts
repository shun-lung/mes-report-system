import { defineStore } from 'pinia'

export interface Factory {
  id: string
  code: string
  name: string
  timezone: string
  permissions: string[]
}
export interface User {
  id: string
  email: string
  name: string
  isSuperAdmin: boolean
}

const KEY = 'mes.session'

function load(): { token: string; factoryId: string } {
  try {
    return JSON.parse(localStorage.getItem(KEY) ?? '') ?? { token: '', factoryId: '' }
  } catch {
    return { token: '', factoryId: '' }
  }
}

function save(token: string, factoryId: string) {
  try {
    localStorage.setItem(KEY, JSON.stringify({ token, factoryId }))
  } catch {
    /* 無法寫入時僅本次有效 */
  }
}

export const useSession = defineStore('session', {
  state: () => ({
    ...load(),
    user: null as User | null,
    factories: [] as Factory[],
  }),
  getters: {
    factory: (s) => s.factories.find((f) => f.id === s.factoryId) ?? null,
  },
  actions: {
    can(perm: string) {
      return this.factory?.permissions.includes(perm) ?? false
    },
    async login(email: string, password: string) {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      })
      const body = await res.json()
      if (!res.ok) throw new Error(body.error ?? '登入失敗')
      this.token = body.token
      await this.loadMe()
    },
    async loadMe() {
      const res = await fetch('/api/auth/me', { headers: { Authorization: `Bearer ${this.token}` } })
      if (!res.ok) {
        this.logout()
        throw new Error('登入已失效')
      }
      const body = await res.json()
      this.user = body.user
      this.factories = body.factories
      if (!this.factories.some((f) => f.id === this.factoryId)) this.factoryId = this.factories[0]?.id ?? ''
      save(this.token, this.factoryId)
    },
    selectFactory(id: string) {
      this.factoryId = id
      save(this.token, id)
    },
    logout() {
      this.token = ''
      this.user = null
      this.factories = []
      save('', this.factoryId)
    },
  },
})
