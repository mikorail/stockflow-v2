import { useDailySales, useSummary } from '../hooks/queries'
import { formatRupiah } from '../lib/format'

export function SummaryCards() {
  const summaryQ = useSummary()
  const dailyQ = useDailySales()
  const summary = summaryQ.data
  const daily = dailyQ.data
  const failed = summaryQ.isError || dailyQ.isError

  const val = (v: string | null | undefined) =>
    v ?? (failed ? <span className="muted">n/a</span> : <span className="skeleton" style={{ width: 64 }} />)

  return (
    <section className="stats" aria-label="Summary">
      <div className="stat lead">
        <div className="stat-label">Stock value</div>
        <div className="stat-value">{val(summary && formatRupiah(summary.total_stock_value))}</div>
      </div>
      <div className="stat">
        <div className="stat-label">Products</div>
        <div className="stat-value">{val(summary && String(summary.total_products))}</div>
      </div>
      <div className="stat">
        <div className="stat-label">Active</div>
        <div className="stat-value">{val(summary && String(summary.active_products))}</div>
      </div>
      <div className={`stat${summary && summary.low_stock_count > 0 ? ' warn' : ''}`}>
        <div className="stat-label">Low stock</div>
        <div className="stat-value">{val(summary && String(summary.low_stock_count))}</div>
      </div>
      <div className="stat">
        <div className="stat-label">Sales today</div>
        <div className="stat-value">{val(daily && formatRupiah(daily.revenue))}</div>
      </div>
      {failed && (
        <div className="banner error" role="alert">
          <span>Summary could not be loaded.</span>
          <button
            className="btn small"
            onClick={() => {
              void summaryQ.refetch()
              void dailyQ.refetch()
            }}
          >
            Retry
          </button>
        </div>
      )}
    </section>
  )
}
