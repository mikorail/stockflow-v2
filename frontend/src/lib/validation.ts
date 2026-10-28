import type { ApiErrorBody, OrderInput } from '../api/types'

export type ParsedQty = { ok: true; value: number } | { ok: false; message: string }

export function parseQty(raw: string): ParsedQty {
  const s = raw.trim()
  if (s === '') return { ok: false, message: 'Quantity is required' }
  if (!/^\d+$/.test(s)) return { ok: false, message: 'Quantity must be a whole number' }
  const n = Number(s)
  if (!Number.isSafeInteger(n) || n <= 0) return { ok: false, message: 'Quantity must be greater than 0' }
  if (n > 2_000_000_000) return { ok: false, message: 'Quantity is too large' }
  return { ok: true, value: n }
}

export interface OrderRowDraft {
  sku: string
  qty: string
}

export interface RowErrors {
  sku?: string
  qty?: string
}

export interface OrderFormErrors {
  customer?: string
  items?: string
  rows: RowErrors[]
}

export function hasOrderErrors(e: OrderFormErrors): boolean {
  return Boolean(e.customer || e.items || e.rows.some((r) => r.sku || r.qty))
}

export function validateOrderForm(customer: string, rows: OrderRowDraft[]): OrderFormErrors {
  const errors: OrderFormErrors = { rows: rows.map(() => ({})) }
  if (customer.trim() === '') errors.customer = 'Customer name is required'
  if (rows.length === 0) errors.items = 'Add at least one item'
  const seen = new Map<string, number>()
  rows.forEach((row, i) => {
    const r = errors.rows[i] as RowErrors
    const sku = row.sku.trim()
    if (sku === '') r.sku = 'SKU is required'
    else if (seen.has(sku)) r.sku = `Duplicate SKU (also row ${(seen.get(sku) as number) + 1})`
    else seen.set(sku, i)
    const q = parseQty(row.qty)
    if (!q.ok) r.qty = q.message
  })
  return errors
}

export function toOrderInput(customer: string, rows: OrderRowDraft[]): OrderInput {
  return {
    customer: customer.trim(),
    items: rows.map((r) => ({ sku: r.sku.trim(), qty: Number(r.qty.trim()) })),
  }
}

export function mapOrderApiError(body: ApiErrorBody, rows: OrderRowDraft[]): OrderFormErrors {
  const errors: OrderFormErrors = { rows: rows.map(() => ({})) }
  if (body.code === 'INSUFFICIENT_STOCK') {
    const idx = rows.findIndex((r) => r.sku.trim() === body.sku)
    const row = errors.rows[idx]
    if (row) row.qty = `Only ${body.available ?? 0} available`
    return errors
  }
  for (const d of body.details ?? []) {
    const m = /^items\[(\d+)\]\.(sku|qty)$/.exec(d.field)
    if (m) {
      const row = errors.rows[Number(m[1])]
      if (row) row[m[2] as 'sku' | 'qty'] = d.message
    } else if (d.field === 'customer') errors.customer = d.message
    else if (d.field === 'items') errors.items = d.message
  }
  return errors
}

export interface ProductFormErrors {
  sku?: string
  name?: string
  price?: string
  min_stock?: string
}

export function validateProductForm(sku: string, name: string, price: string, minStock: string): ProductFormErrors {
  const e: ProductFormErrors = {}
  if (sku.trim() === '') e.sku = 'SKU is required'
  if (name.trim() === '') e.name = 'Name is required'
  const p = price.trim()
  if (p === '') e.price = 'Price is required'
  else if (!/^\d+$/.test(p)) e.price = 'Price must be a whole number of rupiah, 0 or more'
  else if (!Number.isSafeInteger(Number(p)) || Number(p) > 2_147_483_647) e.price = 'Price is too large'
  const m = minStock.trim()
  if (m === '') e.min_stock = 'Low-stock threshold is required'
  else if (!/^\d+$/.test(m)) e.min_stock = 'Threshold must be a whole number, 0 or more'
  else if (Number(m) > 1_000_000) e.min_stock = 'Threshold is too large'
  return e
}

export function mapProductApiError(body: ApiErrorBody): ProductFormErrors {
  if (body.code === 'DUPLICATE_SKU') return { sku: 'This SKU already exists' }
  const e: ProductFormErrors = {}
  for (const d of body.details ?? []) {
    if (d.field === 'sku' || d.field === 'name' || d.field === 'price' || d.field === 'min_stock') e[d.field] = d.message
  }
  return e
}
