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

// upload 以 multipart 上傳單一檔案（欄位名稱 file）。
export async function upload<T>(path: string, file: File): Promise<T> {
  const session = useSession()
  const body = new FormData()
  body.append('file', file)
  const res = await fetch('/api' + path, {
    method: 'POST',
    headers: { Authorization: `Bearer ${session.token}`, 'X-Factory-ID': session.factoryId },
    body,
  })
  if (!res.ok) {
    const j = await res.json().catch(() => ({}))
    throw new ApiError(j.error ?? res.statusText, res.status)
  }
  return res.json()
}

// download 下載檔案（需帶登入標頭，無法用一般連結）；檔名取自 Content-Disposition。
export async function download(path: string, fallbackName: string) {
  const session = useSession()
  const res = await fetch('/api' + path, { headers: { Authorization: `Bearer ${session.token}`, 'X-Factory-ID': session.factoryId } })
  if (!res.ok) {
    const j = await res.json().catch(() => ({}))
    throw new ApiError(j.error ?? res.statusText, res.status)
  }
  const cd = res.headers.get('Content-Disposition') ?? ''
  const m = /filename\*=UTF-8''([^;]+)/.exec(cd)
  const name = m ? decodeURIComponent(m[1]) : fallbackName
  const url = URL.createObjectURL(await res.blob())
  const a = document.createElement('a')
  a.href = url
  a.download = name
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
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
