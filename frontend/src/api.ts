import { useSession } from './stores/session'

export class ApiError extends Error {
  constructor(message: string, public status: number) {
    super(message)
  }
}

export async function api<T>(path: string, opts: { method?: string; body?: unknown } = {}): Promise<T> {
  const session = useSession()
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (session.token) headers.Authorization = `Bearer ${session.token}`
  if (session.factoryId) headers['X-Factory-ID'] = session.factoryId
  const res = await fetch('/api' + path, {
    method: opts.method ?? 'GET',
    headers,
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  })
  if (res.status === 401 && session.token) {
    session.logout()
    window.location.assign('/login')
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new ApiError(body.error ?? res.statusText, res.status)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

export const fmt = {
  int: (v: number) => Math.round(v).toLocaleString('zh-TW'),
  dec: (v: number, d = 1) => v.toLocaleString('zh-TW', { minimumFractionDigits: d, maximumFractionDigits: d }),
  pct: (v: number) => (v * 100).toFixed(1) + '%',
  time(v: string | Date, tz: string) {
    return new Intl.DateTimeFormat('zh-TW', { timeZone: tz, hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(v))
  },
  dateTime(v: string | Date, tz: string) {
    return new Intl.DateTimeFormat('zh-TW', {
      timeZone: tz, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
    }).format(new Date(v))
  },
  date: (v: string | null | undefined) => (v ? v.slice(0, 10) : '—'),
}
