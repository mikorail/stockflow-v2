import { useState } from 'react'
import type { AuditEntity } from '../api/types'
import { useAudit } from '../hooks/queries'
import { ACTION_LABELS, describeAudit } from '../lib/audit'

const LIMIT = 15
const FILTERS: { value: AuditEntity | ''; label: string }[] = [
  { value: '', label: 'All' },
  { value: 'product', label: 'Products and stock' },
  { value: 'order', label: 'Orders' },
]

export function AuditPage() {
  const [entity, setEntity] = useState<AuditEntity | ''>('')
  const [page, setPage] = useState(1)
  const q = useAudit({ entity, page, limit: LIMIT })
  const data = q.data
  const totalPages = data ? Math.max(1, Math.ceil(data.total / data.limit)) : 1

  return (
    <section>
      <div className="toolbar">
        <div className="chips" role="group" aria-label="Filter audit log">
          {FILTERS.map((f) => (
            <button
              key={f.value}
              className={`chip${entity === f.value ? ' on' : ''}`}
              aria-pressed={entity === f.value}
              onClick={() => {
                setEntity(f.value)
                setPage(1)
              }}
            >
              {f.label}
            </button>
          ))}
        </div>
        <span className="muted">Changes appear here within a moment of being saved.</span>
      </div>

      {q.isError && (
        <div className="banner error" role="alert">
          <span>{q.error.message}</span>
          <button className="btn small" onClick={() => void q.refetch()}>
            Retry
          </button>
        </div>
      )}

      <div className="table-wrap">
        <table className="stack">
          <thead>
            <tr>
              <th>When</th>
              <th>Action</th>
              <th>What changed</th>
            </tr>
          </thead>
          <tbody>
            {q.isPending
              ? Array.from({ length: 6 }, (_, i) => (
                  <tr key={i} aria-hidden="true">
                    {Array.from({ length: 3 }, (_, j) => (
                      <td key={j} data-label="">
                        <span className="skeleton" />
                      </td>
                    ))}
                  </tr>
                ))
              : (data?.data ?? []).map((e) => (
                  <tr key={e.id} className={q.isPlaceholderData ? 'stale' : ''}>
                    <td className="muted" data-label="When" title={e.created_at}>
                      {new Date(e.created_at).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })}
                    </td>
                    <td data-label="Action">
                      <span className={`badge ${e.action.endsWith('deleted') || e.action.endsWith('cancelled') ? 'off' : 'ok'}`}>
                        {ACTION_LABELS[e.action] ?? e.action}
                      </span>
                    </td>
                    <td className="wrap" data-label="">{describeAudit(e)}</td>
                  </tr>
                ))}
          </tbody>
        </table>
        {!q.isPending && !q.isError && data?.data.length === 0 && (
          <div className="empty">
            <strong>No audit entries yet</strong>
            <span>Changes to products, stock and orders will appear here.</span>
          </div>
        )}
      </div>

      {data && data.total > 0 && (
        <div className="pager">
          <span className="muted">
            {data.total} entr{data.total === 1 ? 'y' : 'ies'} · page {data.page} of {totalPages}
          </span>
          <button className="btn small" disabled={page <= 1} onClick={() => setPage(page - 1)}>
            Previous
          </button>
          <button className="btn small" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
            Next
          </button>
        </div>
      )}
    </section>
  )
}
