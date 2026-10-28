import { useState } from 'react'
import { useOrders } from '../hooks/queries'
import { formatRupiah, timeAgo } from '../lib/format'
import { CreateOrderModal } from './CreateOrderModal'
import { OrderDetailModal } from './OrderDetailModal'

export function OrdersPage() {
  const q = useOrders()
  const orders = q.data
  const error = q.isError ? q.error.message : null
  const [creating, setCreating] = useState(false)
  const [openId, setOpenId] = useState<number | null>(null)

  return (
    <section>
      <div className="toolbar">
        <button className="btn primary" onClick={() => setCreating(true)}>
          + New order
        </button>
      </div>

      {error && (
        <div className="banner error" role="alert">
          <span>{error}</span>
          <button className="btn small" onClick={() => void q.refetch()}>
            Retry
          </button>
        </div>
      )}

      <div className="table-wrap">
        <table className="stack">
          <thead>
            <tr>
              <th>Order</th>
              <th>Customer</th>
              <th>Status</th>
              <th className="num">Total</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {q.isPending
              ? Array.from({ length: 4 }, (_, i) => (
                  <tr key={i} aria-hidden="true">
                    {Array.from({ length: 5 }, (_, j) => (
                      <td key={j} data-label="">
                        <span className="skeleton" />
                      </td>
                    ))}
                  </tr>
                ))
              : (orders ?? []).map((o) => (
                  <tr key={o.id} className="clickable" onClick={() => setOpenId(o.id)}>
                    <td data-label="Order">
                      <button className="link" onClick={() => setOpenId(o.id)}>
                        #{o.id}
                      </button>
                    </td>
                    <td className="name" data-label="Customer">{o.customer}</td>
                    <td data-label="Status">
                      <span className={`badge ${o.status === 'CREATED' ? 'ok' : 'off'}`}>{o.status}</span>
                    </td>
                    <td className="num" data-label="Total">{formatRupiah(o.total)}</td>
                    <td className="muted" data-label="Created">{timeAgo(o.created_at)}</td>
                  </tr>
                ))}
          </tbody>
        </table>
        {orders?.length === 0 && (
          <div className="empty">
            <strong>No orders yet</strong>
            <span>Create the first one.</span>
          </div>
        )}
      </div>

      {creating && (
        <CreateOrderModal onClose={() => setCreating(false)} />
      )}
      {openId !== null && <OrderDetailModal orderId={openId} onClose={() => setOpenId(null)} />}
    </section>
  )
}
