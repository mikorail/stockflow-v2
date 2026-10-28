import { useState, type FormEvent } from 'react'
import { isApiError } from '../api/client'
import type { Product } from '../api/types'
import { useCreateProduct, useUpdateProduct } from '../hooks/queries'
import { mapProductApiError, validateProductForm, type ProductFormErrors } from '../lib/validation'
import { Modal } from './Modal'

interface Props {
  product: Product | null
  onClose: () => void
}

export function ProductFormModal({ product, onClose }: Props) {
  const create = useCreateProduct()
  const update = useUpdateProduct()
  const saving = create.isPending || update.isPending
  const [sku, setSku] = useState(product?.sku ?? '')
  const [name, setName] = useState(product?.name ?? '')
  const [price, setPrice] = useState(product ? String(product.price) : '')
  const [minStock, setMinStock] = useState(product ? String(product.min_stock) : '10')
  const [active, setActive] = useState(product?.is_active ?? true)
  const [errors, setErrors] = useState<ProductFormErrors>({})
  const [banner, setBanner] = useState<string | null>(null)

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (saving) return
    const local = validateProductForm(sku, name, price, minStock)
    setErrors(local)
    if (Object.keys(local).length > 0) return
    setBanner(null)
    const input = { sku: sku.trim(), name: name.trim(), price: Number(price.trim()), is_active: active, min_stock: Number(minStock.trim()) }
    try {
      if (product) await update.mutateAsync({ id: product.id, input })
      else await create.mutateAsync(input)
      onClose()
    } catch (err) {
      if (isApiError(err)) {
        const mapped = mapProductApiError(err.body)
        setErrors(mapped)
        if (Object.keys(mapped).length === 0) setBanner(err.message)
      } else {
        setBanner('Something went wrong. Please try again.')
      }
    }
  }

  return (
    <Modal title={product ? `Edit product: ${product.sku}` : 'Add product'} onClose={onClose} busy={saving}>
      <form onSubmit={submit} noValidate>
        <label className="field">
          <span>SKU</span>
          <input value={sku} onChange={(e) => setSku(e.target.value)} disabled={saving} autoFocus aria-invalid={Boolean(errors.sku)} />
          {errors.sku && <small className="err">{errors.sku}</small>}
        </label>
        <label className="field">
          <span>Name</span>
          <input value={name} onChange={(e) => setName(e.target.value)} disabled={saving} aria-invalid={Boolean(errors.name)} />
          {errors.name && <small className="err">{errors.name}</small>}
        </label>
        <label className="field">
          <span>Price (Rp, whole number)</span>
          <input inputMode="numeric" value={price} onChange={(e) => setPrice(e.target.value)} disabled={saving} aria-invalid={Boolean(errors.price)} />
          {errors.price && <small className="err">{errors.price}</small>}
        </label>
        <label className="field">
          <span>Low-stock threshold (flag when stock is at or below)</span>
          <input inputMode="numeric" value={minStock} onChange={(e) => setMinStock(e.target.value)} disabled={saving} aria-invalid={Boolean(errors.min_stock)} />
          {errors.min_stock && <small className="err">{errors.min_stock}</small>}
        </label>
        <label className="check">
          <input type="checkbox" checked={active} onChange={(e) => setActive(e.target.checked)} disabled={saving} />
          <span>Active (can be ordered)</span>
        </label>
        {!product && <p className="muted">New products start with 0 stock. Use Adjust stock to add the first units.</p>}
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
            {saving ? 'Saving…' : product ? 'Save changes' : 'Add product'}
          </button>
        </footer>
      </form>
    </Modal>
  )
}
