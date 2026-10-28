import { useEffect, useState } from 'react'
import { AuditPage } from './components/AuditPage'
import { OrdersPage } from './components/OrdersPage'
import { ProductsPage } from './components/ProductsPage'
import { SummaryCards } from './components/SummaryCards'

type Tab = 'products' | 'orders' | 'audit'
type Theme = 'light' | 'dark'

function initialTheme(): Theme {
  try {
    const saved = localStorage.getItem('theme')
    if (saved === 'light' || saved === 'dark') return saved
  } catch {
  }
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export default function App() {
  const [tab, setTab] = useState<Tab>('products')
  const [theme, setTheme] = useState<Theme>(initialTheme)
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    try {
      localStorage.setItem('theme', theme)
    } catch {
    }
  }, [theme])

  return (
    <div className="app">
      <header className="app-head">
        <h1>StockFlow</h1>
        <nav className="tabs">
          <button className={tab === 'products' ? 'on' : ''} onClick={() => setTab('products')}>
            Products
          </button>
          <button className={tab === 'orders' ? 'on' : ''} onClick={() => setTab('orders')}>
            Orders
          </button>
          <button className={tab === 'audit' ? 'on' : ''} onClick={() => setTab('audit')}>
            Audit log
          </button>
        </nav>
        <button
          className="btn small theme-btn"
          onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
          aria-label={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}
        >
          {theme === 'dark' ? 'Light theme' : 'Dark theme'}
        </button>
      </header>
      <SummaryCards />
      {tab === 'products' && <ProductsPage />}
      {tab === 'orders' && <OrdersPage />}
      {tab === 'audit' && <AuditPage />}
    </div>
  )
}
