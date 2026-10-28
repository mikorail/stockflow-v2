import type { Product } from '../api/types'
import { useMovements } from '../hooks/queries'
import { timeAgo } from '../lib/format'
import { Modal } from './Modal'

interface Props {
  product: Product
  onClose: () => void
}

export function MovementsModal({ product, onClose }: Props) {
  const q = useMovements(product.id)
  const rows = q.data ?? []
  return (
    <Modal title={`Stock history: ${product.sku}`} onClose={onClose} wide>
      <p className="muted">
        {product.name} · current stock <strong>{product.stock}</strong>, low-stock threshold {product.min_stock}
      </p>
      {q.isError && (
        <div className="banner error" role="alert">
          <span>{q.error.message}</span>
          <button className="btn small" onClick={() => void q.refetch()}>
            Retry
          </button>
        </div>
      )}
      {q.isPending && <span className="skeleton block" />}
      {q.data && rows.length === 0 && (
        <div className="empty">
          <strong>No movements yet</strong>
          <span>Stock changes for this product will be listed here.</span>
        </div>
      )}
      {rows.length > 0 && (
        <div className="table-wrap history">
          <table className="stack">
            <thead>
              <tr>
                <th>When</th>
                <th>Type</th>
                <th className="num">Qty</th>
                <th>Note</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((m) => (
                <tr key={m.id}>
                  <td className="muted" data-label="When" title={m.created_at}>
                    {timeAgo(m.created_at)}
                  </td>
                  <td data-label="Type">
                    <span className={`badge ${m.type === 'in' ? 'ok' : 'off'}`}>{m.type === 'in' ? 'Stock in' : 'Stock out'}</span>
                  </td>
                  <td className="num" data-label="Qty">
                    {m.type === 'in' ? '+' : '-'}
                    {m.qty}
                  </td>
                  <td className="name" data-label="Note">
                    {m.note ?? ''}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <footer className="modal-foot">
        <button className="btn" onClick={onClose}>
          Close
        </button>
      </footer>
    </Modal>
  )
}
