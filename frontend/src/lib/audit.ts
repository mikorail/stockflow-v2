import type { AuditEntry } from '../api/types'
import { formatRupiah } from './format'

const num = (d: Record<string, unknown>, k: string): number | null => (typeof d[k] === 'number' ? (d[k] as number) : null)
const str = (d: Record<string, unknown>, k: string): string | null => (typeof d[k] === 'string' ? (d[k] as string) : null)

export const ACTION_LABELS: Record<string, string> = {
  'product.created': 'Product created',
  'product.updated': 'Product updated',
  'product.deleted': 'Product deleted',
  'stock.in': 'Stock in',
  'stock.out': 'Stock out',
  'order.created': 'Order created',
  'order.cancelled': 'Order cancelled',
}

function fmtField(field: string, v: unknown): string {
  if (field === 'price' && typeof v === 'number') return formatRupiah(v)
  if (field === 'is_active' && typeof v === 'boolean') return v ? 'active' : 'inactive'
  return String(v)
}

export function describeAudit(e: AuditEntry): string {
  const d = e.details
  switch (e.action) {
    case 'product.created': {
      const price = num(d, 'price')
      return `${e.entity_label} "${str(d, 'name') ?? ''}"${price === null ? '' : ` at ${formatRupiah(price)}`}`
    }
    case 'product.updated': {
      const changes = d['changes']
      if (typeof changes !== 'object' || changes === null) return e.entity_label
      const parts = Object.entries(changes as Record<string, { from?: unknown; to?: unknown }>).map(
        ([field, c]) => `${field}: ${fmtField(field, c.from)} -> ${fmtField(field, c.to)}`,
      )
      return `${e.entity_label}, ${parts.join('; ')}`
    }
    case 'product.deleted':
      return `${e.entity_label} "${str(d, 'name') ?? ''}" removed`
    case 'stock.in':
    case 'stock.out': {
      const sign = e.action === 'stock.in' ? '+' : '-'
      const note = str(d, 'note')
      return `${e.entity_label} ${sign}${num(d, 'qty') ?? '?'} (${num(d, 'stock_before') ?? '?'} -> ${num(d, 'stock_after') ?? '?'})${note ? `, "${note}"` : ''}`
    }
    case 'order.created': {
      const total = num(d, 'total')
      return `${e.entity_label} for ${str(d, 'customer') ?? 'unknown customer'}${total === null ? '' : `, total ${formatRupiah(total)}`}`
    }
    case 'order.cancelled':
      return `${e.entity_label} cancelled, stock restored`
    default:
      return e.entity_label
  }
}
