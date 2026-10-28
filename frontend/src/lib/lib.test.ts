import { describe, expect, it } from 'vitest'
import { formatRupiah, timeAgo } from './format'
import { hasOrderErrors, mapOrderApiError, parseQty, toOrderInput, validateOrderForm } from './validation'

describe('parseQty', () => {
  it('accepts positive integers', () => {
    expect(parseQty('5')).toEqual({ ok: true, value: 5 })
    expect(parseQty(' 12 ')).toEqual({ ok: true, value: 12 })
  })
  it.each(['', '  ', '0', '-3', '2.5', '1e3', 'abc'])('rejects %j', (raw) => {
    expect(parseQty(raw).ok).toBe(false)
  })
  it('rejects absurdly large numbers', () => {
    expect(parseQty('99999999999999999999').ok).toBe(false)
  })
})

describe('validateOrderForm', () => {
  it('passes a valid form', () => {
    const e = validateOrderForm('Budi', [{ sku: 'A', qty: '2' }])
    expect(hasOrderErrors(e)).toBe(false)
  })
  it('flags missing customer, empty items', () => {
    const e = validateOrderForm(' ', [])
    expect(e.customer).toBeTruthy()
    expect(e.items).toBeTruthy()
  })
  it('flags bad qty and duplicate sku on the right rows', () => {
    const e = validateOrderForm('x', [
      { sku: 'A', qty: '1' },
      { sku: 'A', qty: '0' },
      { sku: '', qty: '1' },
    ])
    expect(e.rows[0]).toEqual({})
    expect(e.rows[1]?.sku).toMatch(/Duplicate/)
    expect(e.rows[1]?.qty).toBeTruthy()
    expect(e.rows[2]?.sku).toBeTruthy()
  })
  it('builds a trimmed numeric payload', () => {
    expect(toOrderInput(' Budi ', [{ sku: ' A ', qty: ' 3 ' }])).toEqual({
      customer: 'Budi',
      items: [{ sku: 'A', qty: 3 }],
    })
  })
})

describe('mapOrderApiError', () => {
  const rows = [
    { sku: 'A', qty: '1' },
    { sku: 'B', qty: '9' },
  ]
  it('highlights the failing SKU with available qty on 409', () => {
    const e = mapOrderApiError({ code: 'INSUFFICIENT_STOCK', message: '', sku: 'B', available: 4 }, rows)
    expect(e.rows[0]).toEqual({})
    expect(e.rows[1]?.qty).toBe('Only 4 available')
  })
  it('maps 400 details onto rows and fields', () => {
    const e = mapOrderApiError(
      {
        code: 'VALIDATION_ERROR',
        message: '',
        details: [
          { field: 'customer', message: 'customer is required' },
          { field: 'items[0].sku', message: 'unknown sku "A"' },
        ],
      },
      rows,
    )
    expect(e.customer).toBe('customer is required')
    expect(e.rows[0]?.sku).toBe('unknown sku "A"')
  })
})

describe('formatRupiah', () => {
  it('groups thousands with dots', () => {
    expect(formatRupiah(1234567)).toBe('Rp 1.234.567')
    expect(formatRupiah(0)).toBe('Rp 0')
    expect(formatRupiah(850000)).toBe('Rp 850.000')
  })
})

describe('timeAgo', () => {
  const now = Date.parse('2026-01-01T12:00:00Z')
  it('formats relative times', () => {
    expect(timeAgo('2026-01-01T11:59:50Z', now)).toBe('just now')
    expect(timeAgo('2026-01-01T11:55:00Z', now)).toBe('5m ago')
    expect(timeAgo('2026-01-01T10:00:00Z', now)).toBe('2h ago')
    expect(timeAgo('2025-12-29T12:00:00Z', now)).toBe('3d ago')
  })
  it('handles null', () => {
    expect(timeAgo(null, now)).toBe('never')
  })
})

import type { AuditEntry } from '../api/types'
import { describeAudit } from './audit'
import { mapProductApiError, validateProductForm } from './validation'

describe('validateProductForm', () => {
  it('accepts a valid product, price 0 included', () => {
    expect(validateProductForm('A-1', 'Thing', '15000', '10')).toEqual({})
    expect(validateProductForm('A-1', 'Thing', '0', '0')).toEqual({})
  })
  it('flags empty fields and non-integer prices', () => {
    const e = validateProductForm(' ', '', '10.5', '10')
    expect(e.sku).toBeTruthy()
    expect(e.name).toBeTruthy()
    expect(e.price).toBeTruthy()
    expect(validateProductForm('a', 'b', '-1', '10').price).toBeTruthy()
    expect(validateProductForm('a', 'b', '', '10').price).toBeTruthy()
    expect(validateProductForm('a', 'b', '99999999999', '10').price).toBeTruthy()
  })
  it('validates the low-stock threshold', () => {
    expect(validateProductForm('a', 'b', '1', '').min_stock).toBeTruthy()
    expect(validateProductForm('a', 'b', '1', '-1').min_stock).toBeTruthy()
    expect(validateProductForm('a', 'b', '1', '2.5').min_stock).toBeTruthy()
    expect(validateProductForm('a', 'b', '1', '0').min_stock).toBeUndefined()
  })
})

describe('mapProductApiError', () => {
  it('puts DUPLICATE_SKU on the sku field', () => {
    expect(mapProductApiError({ code: 'DUPLICATE_SKU', message: '' }).sku).toBeTruthy()
  })
  it('maps validation details by field', () => {
    const e = mapProductApiError({ code: 'VALIDATION_ERROR', message: '', details: [{ field: 'price', message: 'price must be >= 0' }] })
    expect(e.price).toBe('price must be >= 0')
  })
})

describe('describeAudit', () => {
  const base = { id: 1, entity_type: 'product', entity_id: 1, entity_label: 'KB-001', created_at: '' } as const
  const entry = (action: string, details: Record<string, unknown>): AuditEntry => ({ ...base, action, details })
  it('describes stock moves with before/after', () => {
    expect(describeAudit(entry('stock.out', { qty: 3, stock_before: 10, stock_after: 7, note: 'damaged' }))).toBe(
      'KB-001 -3 (10 -> 7), "damaged"',
    )
  })
  it('describes updates field by field', () => {
    const s = describeAudit(entry('product.updated', { changes: { price: { from: 1000, to: 2000 }, is_active: { from: true, to: false } } }))
    expect(s).toContain('price: Rp 1.000 -> Rp 2.000')
    expect(s).toContain('is_active: active -> inactive')
  })
  it('survives missing details', () => {
    expect(describeAudit(entry('order.created', {}))).toContain('unknown customer')
    expect(describeAudit(entry('something.new', {}))).toBe('KB-001')
  })
})
