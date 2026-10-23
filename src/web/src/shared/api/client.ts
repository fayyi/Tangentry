export class APIError extends Error {
  constructor(message: string, readonly code: string, readonly status: number, readonly details: unknown[] = [], readonly payload?: unknown) {
    super(message)
  }
}

type Fallback = { message: string; code: string }
const defaultFallback: Fallback = { message: '操作未能完成，请重试。', code: 'unknown_error' }

// Sends an API request and returns the successful response for callers that
// read a non-JSON body (files, archives). Failures become APIError.
export async function requestResponse(path: string, options: RequestInit = {}, fallback = defaultFallback): Promise<Response> {
  let response: Response
  try {
    response = await fetch(`/api${path}`, {
      ...options,
      headers: { ...(options.body && typeof options.body === 'string' ? { 'Content-Type': 'application/json' } : {}), ...options.headers },
    })
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw error
    if (options.signal?.aborted) throw error
    throw new APIError('无法连接 Tangentry，请确认服务仍在运行。', 'network_error', 0)
  }
  if (!response.ok) {
    const error = await response.json().catch(() => ({})) as { message?: string; code?: string; details?: unknown }
    if (response.status === 401 && error.code === 'unauthorized' && typeof window !== 'undefined') {
      window.location.assign('/login')
    }
    throw new APIError(error.message ?? fallback.message, error.code ?? fallback.code, response.status, Array.isArray(error.details) ? error.details : [], error)
  }
  return response
}

export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await requestResponse(path, options)
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}
