import { useState } from 'react'
import type { Product, StatusFilter } from '../api/types'
import { useProducts } from '../hooks/queries'
import { useDebounce } from '../hooks/useDebounce'
import { formatRupiah, timeAgo } from '../lib/format'
import { useUpdateProduct } from '../hooks/queries'
import { AdjustStockModal } from './AdjustStockModal'
import { DeleteProductModal } from './DeleteProductModal'
import { MovementsModal } from './MovementsModal'
import { ProductFormModal } from './ProductFormModal'

const LIMIT = 10
const FILTERS: { value: StatusFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'active', label: 'Active' },
  { value: 'inactive', label: 'Inactive' },
  { value: 'low', label: 'Low stock' },
]

export function ProductsPage() {
  const [searchInput, setSearchInput] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [page, setPage] = useState(1)
  const [adjusting, setAdjusting] = useState<Product | null>(null)
  const [editing, setEditing] = useState<Product | 'new' | null>(null)
  const [deleting, setDeleting] = useState<Product | null>(null)
  const [history, setHistory] = useState<Product | null>(null)
  const update = useUpdateProduct()

  const search = useDebounce(searchInput, 300)

  const q = useProducts({ search, status, page, limit: LIMIT })
  const data = q.data
  const loading = q.isFetching
  const error = q.isError ? q.error.message : null

  const totalPages = data ? Math.max(1, Math.ceil(data.total / data.limit)) : 1
  const rows = data?.data ?? []

  return (
    <section>
      <div className="toolbar">
        <input
          type="search"
          className="search"
          placeholder="Search SKU or name…"
          value={searchInput}
          onChange={(e) => {
            setSearchInput(e.target.value)
            setPage(1)
          }}
          aria-label="Search products"
        />
        <button className="btn primary" onClick={() => setEditing('new')}>
          + Add product
        </button>
        <div className="chips" role="group" aria-label="Filter">
          {FILTERS.map((f) => (
            <button
              key={f.value}
              className={`chip${status === f.value ? ' on' : ''}`}
              aria-pressed={status === f.value}
              onClick={() => {
                setStatus(f.value)
                setPage(1)
              }}
            >
              {f.label}
            </button>
          ))}
        </div>
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
        <table className="stack products">
          <thead>
            <tr>
              <th>SKU</th>
              <th>Name</th>
              <th className="num">Price</th>
              <th className="num">Stock</th>
              <th>Status</th>
              <th className="hide-md">Last movement</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {q.isPending
              ? Array.from({ length: 6 }, (_, i) => (
                  <tr key={i} aria-hidden="true">
                    {Array.from({ length: 7 }, (_, j) => (
                      <td key={j} data-label="">
                        <span className="skeleton" />
                      </td>
                    ))}
                  </tr>
                ))
              : rows.map((p) => (
                  <tr key={p.id} className={q.isPlaceholderData ? 'stale' : ''}>
                    <td className="mono" data-label="">{p.sku}</td>
                    <td className="name" data-label="">{p.name}</td>
                    <td className="num" data-label="Price">{formatRupiah(p.price)}</td>
                    <td className={`num${p.stock <= p.min_stock ? ' low' : ''}`} data-label="Stock">{p.stock}</td>
                    <td className="status" data-label="">
                      <span className={`badge ${p.is_active ? 'ok' : 'off'}`}>{p.is_active ? 'Active' : 'Inactive'}</span>
                    </td>
                    <td className="muted hide-md" data-label="Last movement" title={p.last_movement_at ?? undefined}>
                      {timeAgo(p.last_movement_at)}
                    </td>
                    <td className="actions" data-label="">
                      <button className="btn small" onClick={() => setAdjusting(p)}>
                        Adjust stock
                      </button>{' '}
                      <button className="btn small" onClick={() => setHistory(p)}>
                        History
                      </button>{' '}
                      <button className="btn small" onClick={() => setEditing(p)}>
                        Edit
                      </button>{' '}
                      <button className="btn small danger" onClick={() => setDeleting(p)}>
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
          </tbody>
        </table>

        {!loading && !error && rows.length === 0 && (
          <div className="empty">
            <strong>No products found</strong>
            <span>{search || status !== 'all' ? 'Try a different search or filter.' : 'Nothing here yet.'}</span>
          </div>
        )}
        {!loading && error && rows.length === 0 && (
          <div className="empty">
            <strong>Couldn’t load products</strong>
            <span>The list is unavailable right now.</span>
          </div>
        )}
      </div>

      {data && data.total > 0 && (
        <div className="pager">
          <span className="muted">
            {data.total} product{data.total === 1 ? '' : 's'} · page {data.page} of {totalPages}
          </span>
          <button className="btn small" disabled={page <= 1} onClick={() => setPage(page - 1)}>
            Previous
          </button>
          <button className="btn small" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
            Next
          </button>
        </div>
      )}

      {adjusting && (
        <AdjustStockModal
          product={adjusting}
          onClose={() => setAdjusting(null)}
          onDone={() => setAdjusting(null)}
        />
      )}
      {history && <MovementsModal product={history} onClose={() => setHistory(null)} />}
      {editing && <ProductFormModal product={editing === 'new' ? null : editing} onClose={() => setEditing(null)} />}
      {deleting && (
        <DeleteProductModal
          product={deleting}
          onClose={() => setDeleting(null)}
          onMarkInactive={(p) => {
            update.mutate(
              { id: p.id, input: { sku: p.sku, name: p.name, price: p.price, is_active: false, min_stock: p.min_stock } },
              { onSuccess: () => setDeleting(null) },
            )
          }}
        />
      )}
    </section>
  )
}
