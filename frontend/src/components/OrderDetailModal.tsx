import { useState } from 'react'
import { isApiError } from '../api/client'
import { useCancelOrder, useOrder } from '../hooks/queries'
import { timeAgo } from '../lib/format'
import { ItemsTable } from './CreateOrderModal'
import { Modal } from './Modal'

interface Props {
  orderId: number
  onClose: () => void
}

export function OrderDetailModal({ orderId, onClose }: Props) {
  const q = useOrder(orderId)
  const cancel = useCancelOrder()
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [justCancelled, setJustCancelled] = useState(false)
  const order = q.data
  const cancelling = cancel.isPending

  async function doCancel() {
    setError(null)
    try {
      await cancel.mutateAsync(orderId)
      setJustCancelled(true)
      setConfirming(false)
    } catch (e) {
      setConfirming(false)
      if (isApiError(e) && e.code === 'ORDER_ALREADY_CANCELLED') setError('This order was already cancelled.')
      else setError(isApiError(e) ? e.message : 'Cancel failed. Please try again.')
    }
  }

  return (
    <Modal title={`Order #${orderId}`} onClose={onClose} busy={cancelling} wide>
      {q.isError && (
        <div className="banner error" role="alert">
          <span>{q.error.message}</span>
          <button className="btn small" onClick={() => void q.refetch()}>
            Retry
          </button>
        </div>
      )}
      {q.isPending && <span className="skeleton block" />}
      {order && (
        <>
          <p className="muted">
            {order.customer} · {timeAgo(order.created_at)} ·{' '}
            <span className={`badge ${order.status === 'CREATED' ? 'ok' : 'off'}`}>{order.status}</span>
          </p>
          <ItemsTable order={order} />
          {justCancelled && <div className="banner ok">Order cancelled. Stock has been restored.</div>}
          {error && (
            <div className="banner error" role="alert">
              {error}
            </div>
          )}
          <footer className="modal-foot">
            {order.status === 'CREATED' && !confirming && (
              <button className="btn danger" onClick={() => setConfirming(true)}>
                Cancel order
              </button>
            )}
            {confirming && (
              <>
                <span className="muted">Cancel this order and restore stock?</span>
                <button className="btn" onClick={() => setConfirming(false)} disabled={cancelling}>
                  Keep order
                </button>
                <button className="btn danger" onClick={() => void doCancel()} disabled={cancelling}>
                  {cancelling ? 'Cancelling…' : 'Yes, cancel order'}
                </button>
              </>
            )}
            {!confirming && (
              <button className="btn" onClick={onClose}>
                Close
              </button>
            )}
          </footer>
        </>
      )}
    </Modal>
  )
}
