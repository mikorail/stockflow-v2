import { useState } from 'react'
import { isApiError } from '../api/client'
import type { Product } from '../api/types'
import { useDeleteProduct } from '../hooks/queries'
import { Modal } from './Modal'

interface Props {
  product: Product
  onClose: () => void
  onMarkInactive: (p: Product) => void
}

export function DeleteProductModal({ product, onClose, onMarkInactive }: Props) {
  const del = useDeleteProduct()
  const [error, setError] = useState<{ message: string; hasHistory: boolean } | null>(null)

  async function confirm() {
    setError(null)
    try {
      await del.mutateAsync(product.id)
      onClose()
    } catch (e) {
      if (isApiError(e) && e.code === 'PRODUCT_HAS_HISTORY') {
        setError({ message: 'This product has stock or order history, so it cannot be deleted. History is never removed.', hasHistory: true })
      } else {
        setError({ message: isApiError(e) ? e.message : 'Delete failed. Please try again.', hasHistory: false })
      }
    }
  }

  return (
    <Modal title={`Delete ${product.sku}?`} onClose={onClose} busy={del.isPending}>
      <p>
        Delete <strong>{product.name}</strong>? This cannot be undone.
      </p>
      {error && (
        <div className="banner error" role="alert">
          {error.message}
        </div>
      )}
      <footer className="modal-foot">
        <button className="btn" onClick={onClose} disabled={del.isPending}>
          Cancel
        </button>
        {error?.hasHistory && product.is_active && (
          <button className="btn" onClick={() => onMarkInactive(product)}>
            Mark inactive instead
          </button>
        )}
        {!error?.hasHistory && (
          <button className="btn danger" onClick={() => void confirm()} disabled={del.isPending}>
            {del.isPending ? 'Deleting…' : 'Delete product'}
          </button>
        )}
      </footer>
    </Modal>
  )
}
