import type {
  ApiErrorBody,
  AuditEntity,
  AuditEntry,
  DailySales,
  Movement,
  Order,
  OrderInput,
  Page,
  Product,
  ProductInput,
  StatusFilter,
  StockAdjustResult,
  StockAdjustment,
  Summary,
} from './types'

export class ApiError extends Error {
  readonly status: number
  readonly body: ApiErrorBody

  constructor(status: number, body: ApiErrorBody) {
    super(body.message)
    this.name = 'ApiError'
    this.status = status
    this.body = body
  }

  get code(): string {
    return this.body.code
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError
}

function isErrorEnvelope(v: unknown): v is { error: ApiErrorBody } {
  if (typeof v !== 'object' || v === null || !('error' in v)) return false
  const e = (v as { error: unknown }).error
  return typeof e === 'object' && e !== null && typeof (e as { code?: unknown }).code === 'string'
}

interface RequestOptions {
  body?: unknown
  headers?: Record<string, string>
  signal?: AbortSignal
}

async function request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
  let res: Response
  try {
    res = await fetch(`/api${path}`, {
      method,
      signal: opts.signal,
      headers: {
        ...(opts.body !== undefined ? { 'Content-Type': 'application/json' } : {}),
        ...opts.headers,
      },
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    })
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e
    throw new ApiError(0, { code: 'NETWORK', message: 'Cannot reach the server. Check your connection and retry.' })
  }

  if (res.status === 204) return undefined as T

  let payload: unknown = null
  try {
    payload = await res.json()
  } catch {
  }
  if (!res.ok) {
    if (isErrorEnvelope(payload)) throw new ApiError(res.status, payload.error)
    throw new ApiError(res.status, { code: 'UNEXPECTED', message: `Unexpected server response (${res.status})` })
  }
  return payload as T
}

export interface ListProductsParams {
  search: string
  status: StatusFilter
  page: number
  limit: number
}

function qs(params: Record<string, string | number>): string {
  const u = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== '') u.set(k, String(v))
  const s = u.toString()
  return s ? `?${s}` : ''
}

export const api = {
  listProducts: (p: ListProductsParams, signal?: AbortSignal) =>
    request<Page<Product>>('GET', `/products${qs({
      search: p.search,
      status: p.status === 'all' ? '' : p.status,
      page: p.page,
      limit: p.limit,
    })}`, { signal }),

  createProduct: (input: ProductInput) => request<Product>('POST', '/products', { body: input }),

  updateProduct: (id: number, input: ProductInput) => request<Product>('PUT', `/products/${id}`, { body: input }),

  deleteProduct: (id: number) => request<void>('DELETE', `/products/${id}`),

  listAudit: (p: { entity: AuditEntity | ''; page: number; limit: number }, signal?: AbortSignal) =>
    request<Page<AuditEntry>>('GET', `/audit${qs({ entity: p.entity, page: p.page, limit: p.limit })}`, { signal }),

  adjustStock: (id: number, adj: StockAdjustment) =>
    request<StockAdjustResult>('POST', `/products/${id}/stock`, { body: adj }),

  listMovements: (id: number) => request<{ data: Movement[] }>('GET', `/products/${id}/movements`),

  summary: (signal?: AbortSignal) => request<Summary>('GET', '/summary', { signal }),

  dailySales: (signal?: AbortSignal) => request<DailySales>('GET', '/reports/daily-sales', { signal }),

  listOrders: (signal?: AbortSignal) => request<{ data: Order[] }>('GET', '/orders', { signal }),

  getOrder: (id: number) => request<Order>('GET', `/orders/${id}`),

  createOrder: (input: OrderInput, idempotencyKey: string) =>
    request<Order>('POST', '/orders', { body: input, headers: { 'Idempotency-Key': idempotencyKey } }),

  cancelOrder: (id: number) => request<Order>('POST', `/orders/${id}/cancel`),
}
