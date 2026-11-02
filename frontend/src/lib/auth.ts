export type AuthState = { required: boolean; code: string }
let state: AuthState = { required: false, code: '' }
const listeners = new Set<() => void>()
let waiting: Promise<void> | null = null
let release: (() => void) | null = null

export const getAuthState = () => state
export const subscribeAuth = (listener: () => void) => {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function requireAuthentication(code = 'token_required'): Promise<void> {
  if (!waiting)
    waiting = new Promise<void>((resolve) => {
      release = resolve
    })
  state = { required: true, code }
  for (const listener of listeners) listener()
  return waiting
}

export async function authenticate(token: string): Promise<void> {
  const response = await fetch('/api/security/session', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
  })
  if (!response.ok) {
    const failure = (await response.json().catch(() => ({}))) as { error?: string }
    throw new Error(failure.error ?? `Authentication failed (${response.status}).`)
  }
  state = { required: false, code: '' }
  const resolve = release
  waiting = null
  release = null
  resolve?.()
  for (const listener of listeners) listener()
}

// Authentication rejection happens before dispatch, so retrying once cannot
// repeat a successfully executed mutation. Other failures are never retried here.
export async function authenticatedFetch(
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> {
  const original = input instanceof Request ? input.clone() : input
  const retry = original instanceof Request ? original.clone() : original
  const response = await (init === undefined ? fetch(original) : fetch(original, init))
  if (response.status !== 401) return response
  const failure = (await response
    .clone()
    .json()
    .catch(() => ({}))) as { code?: string }
  if (!failure.code?.startsWith('token_')) return response
  await requireAuthentication(failure.code)
  const second = await (init === undefined ? fetch(retry) : fetch(retry, init))
  if (second.status === 401) {
    const failure = (await second
      .clone()
      .json()
      .catch(() => ({}))) as { code?: string }
    if (failure.code?.startsWith('token_')) void requireAuthentication(failure.code)
  }
  return second
}
