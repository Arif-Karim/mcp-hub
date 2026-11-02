import { reactive } from 'vue'

export interface Toast {
  id: number
  kind: 'error' | 'success' | 'info'
  text: string
}

/** App-wide toast queue, rendered by ToastStack (aria-live). */
export const toasts = reactive<Toast[]>([])

let next = 1

export function dismissToast(id: number): void {
  const i = toasts.findIndex((t) => t.id === id)
  if (i >= 0) toasts.splice(i, 1)
}

export function pushToast(kind: Toast['kind'], text: string, ttlMs = 6000): void {
  const id = next++
  toasts.push({ id, kind, text })
  if (toasts.length > 4) toasts.shift()
  setTimeout(() => dismissToast(id), ttlMs)
}
