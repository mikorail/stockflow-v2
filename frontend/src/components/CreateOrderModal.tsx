import { useRef, useState, type FormEvent } from 'react'
import { isApiError } from '../api/client'
import type { Order } from '../api/types'
import { useCreateOrder, useProducts } from '../hooks/queries'
import { formatRupiah } from '../lib/format'
import {
  hasOrderErrors,
  mapOrderApiError,
  toOrderInput,
  validateOrderForm,
  type OrderFormErrors,
  type OrderRowDraft,
} from '../lib/validation'
import { Modal } from './Modal'

interface Props {
  onClose: () => void
}

interface Row extends OrderRowDraft {
  id: number
}

const noErrors = (n: number): OrderFormErrors => ({ rows: Array.from({ length: n }, () => ({})) })

export function CreateOrderModal({ onClose }: Props) {
  const nextId = useRef(1)
  const newRow = (): Row => ({ id: nextId.current++, sku: '', qty: '1' })
  const [customer, setCustomer] = useState('')
  const [rows, setRows] = useState<Row[]>(() => [newRow()])
  const [errors, setErrors] = useState<OrderFormErrors>(noErrors(1))
  const [banner, setBanner] = useState<string | null>(null)
  const mutation = useCreateOrder()
  const saving = mutation.isPending
  const [created, setCreated] = useState<Order | null>(null)
  const products = useProducts({ search: '', status: 'active', page: 1, limit: 100 }).data?.data ?? []
  const idemKey = useRef(crypto.randomUUID())

  const edited = () => {
    idemKey.current = crypto.randomUUID()
    setBanner(null)
  }

  function updateRow(id: number, patch: Partial<OrderRowDraft>) {
    edited()
    setRows((rs) => rs.map((r) => (r.id === id ? { ...r, ...patch } : r)))
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (saving) return
    const local = validateOrderForm(customer, rows)
    setErrors(local)
    if (hasOrderErrors(local)) return
    setBanner(null)
    try {
      const order = await mutation.mutateAsync({ input: toOrderInput(customer, rows), key: idemKey.current })
      setCreated(order)
    } catch (err) {
      if (isApiError(err) && (err.code === 'INSUFFICIENT_STOCK' || err.code === 'VALIDATION_ERROR')) {
        setErrors(mapOrderApiError(err.body, rows))
        if (err.code === 'INSUFFICIENT_STOCK') {
          setBanner(`Not enough stock for ${err.body.sku}: ${err.body.available ?? 0} available. No stock was changed.`)
        } else {
          const msgs = (err.body.details ?? []).map((d) => d.message)
          setBanner(msgs.length ? msgs.join(' · ') : err.message)
        }
      } else {
        setBanner(isApiError(err) ? err.message : 'Something went wrong. Please try again.')
      }
    }
  }

  if (created) {
    return (
      <Modal title={`Order #${created.id} created`} onClose={onClose} wide>
        <div className="banner ok">Order placed. Total {formatRupiah(created.total)} (calculated by the server).</div>
        <ItemsTable order={created} />
        <footer className="modal-foot">
          <button className="btn primary" onClick={onClose}>
            Done
          </button>
        </footer>
      </Modal>
    )
  }

  return (
    <Modal title="New sales order" onClose={onClose} busy={saving} wide>
      <form onSubmit={submit} noValidate>
        <label className="field">
          <span>Customer</span>
          <input
            value={customer}
            onChange={(e) => {
              edited()
              setCustomer(e.target.value)
            }}
            disabled={saving}
            autoFocus
            aria-invalid={Boolean(errors.customer)}
          />
          {errors.customer && <small className="err">{errors.customer}</small>}
        </label>

        <datalist id="sku-options">
          {products.map((p) => (
            <option key={p.id} value={p.sku}>
              {p.name} ({p.stock} in stock)
            </option>
          ))}
        </datalist>

        <div className="order-rows">
          {rows.map((r, i) => {
            const re = errors.rows[i] ?? {}
            return (
              <div className="order-row" key={r.id}>
                <label className="field">
                  <span>SKU</span>
                  <input
                    list="sku-options"
                    value={r.sku}
                    onChange={(e) => updateRow(r.id, { sku: e.target.value })}
                    disabled={saving}
                    aria-invalid={Boolean(re.sku)}
                  />
                  {re.sku && <small className="err">{re.sku}</small>}
                </label>
                <label className="field qty">
                  <span>Qty</span>
                  <input
                    inputMode="numeric"
                    value={r.qty}
                    onChange={(e) => updateRow(r.id, { qty: e.target.value })}
                    disabled={saving}
                    aria-invalid={Boolean(re.qty)}
                  />
                  {re.qty && <small className="err">{re.qty}</small>}
                </label>
                <button
                  type="button"
                  className="icon-btn"
                  aria-label={`Remove row ${i + 1}`}
                  disabled={saving || rows.length === 1}
                  onClick={() => {
                    edited()
                    setRows(rows.filter((x) => x.id !== r.id))
                    setErrors(noErrors(rows.length - 1))
                  }}
                >
                  ×
                </button>
              </div>
            )
          })}
        </div>
        {errors.items && <small className="err">{errors.items}</small>}

        <button
          type="button"
          className="btn small"
          disabled={saving}
          onClick={() => {
            edited()
            setRows([...rows, newRow()])
            setErrors(noErrors(rows.length + 1))
          }}
        >
          + Add item
        </button>

        {banner && (
          <div className="banner error" role="alert">
            {banner}
          </div>
        )}

        <footer className="modal-foot">
          <button type="button" className="btn" onClick={onClose} disabled={saving}>
            Cancel
          </button>
          <button type="submit" className="btn primary" disabled={saving}>
            {saving ? 'Creating…' : 'Create order'}
          </button>
        </footer>
      </form>
    </Modal>
  )
}

export function ItemsTable({ order }: { order: Order }) {
  return (
    <div className="table-wrap">
    <table className="items stack">
      <thead>
        <tr>
          <th>SKU</th>
          <th>Name</th>
          <th className="num">Qty</th>
          <th className="num">Unit price</th>
          <th className="num">Subtotal</th>
        </tr>
      </thead>
      <tbody>
        {order.items.map((it) => (
          <tr key={it.sku}>
            <td className="mono" data-label="SKU">{it.sku}</td>
            <td className="name" data-label="Item">{it.name ?? ''}</td>
            <td className="num" data-label="Qty">{it.qty}</td>
            <td className="num" data-label="Unit price">{formatRupiah(it.unit_price)}</td>
            <td className="num" data-label="Subtotal">{formatRupiah(it.unit_price * it.qty)}</td>
          </tr>
        ))}
      </tbody>
      <tfoot>
        <tr>
          <td colSpan={4} className="total-label">Total</td>
          <td className="num" data-label="Total">
            <strong>{formatRupiah(order.total)}</strong>
          </td>
        </tr>
      </tfoot>
    </table>
    </div>
  )
}
