import { useState, type FormEvent } from 'react'
import { isApiError } from '../api/client'
import { useAdjustStock } from '../hooks/queries'
import type { MovementType, Product } from '../api/types'
import { parseQty } from '../lib/validation'
import { Modal } from './Modal'

interface Props {
  product: Product
  onClose: () => void
  onDone: () => void
}

export function AdjustStockModal({ product, onClose, onDone }: Props) {
  const [type, setType] = useState<MovementType>('in')
  const [qty, setQty] = useState('')
  const [note, setNote] = useState('')
  const mutation = useAdjustStock()
  const saving = mutation.isPending
  const [serverError, setServerError] = useState<string | null>(null)

  const parsed = parseQty(qty)
  const qtyError = !parsed.ok && qty !== '' ? parsed.message : null

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!parsed.ok || saving) return
    setServerError(null)
    try {
      await mutation.mutateAsync({ id: product.id, adj: { type, qty: parsed.value, note } })
      onDone()
    } catch (err) {
      if (isApiError(err) && err.code === 'INSUFFICIENT_STOCK') {
        setServerError(`Insufficient stock: only ${err.body.available ?? 0} available, cannot remove ${parsed.value}.`)
      } else if (isApiError(err)) {
        setServerError(err.body.details?.map((d) => d.message).join('; ') || err.message)
      } else {
        setServerError('Something went wrong. Please try again.')
      }
    }
  }

  return (
    <Modal title={`Adjust stock: ${product.sku}`} onClose={onClose} busy={saving}>
      <form onSubmit={submit} noValidate>
        <p className="muted">
          {product.name} · current stock <strong>{product.stock}</strong>
        </p>

        <fieldset className="seg" disabled={saving}>
          <legend className="sr">Direction</legend>
          <label className={type === 'in' ? 'on' : ''}>
            <input type="radio" name="type" checked={type === 'in'} onChange={() => setType('in')} /> Stock in
          </label>
          <label className={type === 'out' ? 'on' : ''}>
            <input type="radio" name="type" checked={type === 'out'} onChange={() => setType('out')} /> Stock out
          </label>
        </fieldset>

        <label className="field">
          <span>Quantity</span>
          <input
            inputMode="numeric"
            value={qty}
            onChange={(e) => setQty(e.target.value)}
            disabled={saving}
            autoFocus
            aria-invalid={Boolean(qtyError)}
          />
          {qtyError && <small className="err">{qtyError}</small>}
        </label>

        <label className="field">
          <span>Note (optional)</span>
          <input value={note} onChange={(e) => setNote(e.target.value)} disabled={saving} maxLength={200} />
        </label>

        {serverError && (
          <div className="banner error" role="alert">
            {serverError}
          </div>
        )}

        <footer className="modal-foot">
          <button type="button" className="btn" onClick={onClose} disabled={saving}>
            Cancel
          </button>
          <button type="submit" className="btn primary" disabled={!parsed.ok || saving}>
            {saving ? 'Saving…' : 'Save'}
          </button>
        </footer>
      </form>
    </Modal>
  )
}
