
export interface Product {
  id: number
  sku: string
  name: string
  price: number
  stock: number
  is_active: boolean
  min_stock: number
  created_at: string
  last_movement_at: string | null
}

export type StatusFilter = 'all' | 'active' | 'inactive' | 'low'

export interface Page<T> {
  data: T[]
  page: number
  limit: number
  total: number
}

export type MovementType = 'in' | 'out'

export interface Movement {
  id: number
  product_id: number
  type: MovementType
  qty: number
  note: string | null
  created_at: string
}

export interface StockAdjustment {
  type: MovementType
  qty: number
  note: string
}

export interface StockAdjustResult {
  product: Product
  movement: Movement
}

export interface Summary {
  total_products: number
  active_products: number
  total_stock_value: number
  low_stock_count: number
}

export type OrderStatus = 'CREATED' | 'CANCELLED'

export interface OrderItem {
  sku: string
  name?: string
  qty: number
  unit_price: number
}

export interface Order {
  id: number
  customer: string
  status: OrderStatus
  total: number
  created_at: string
  cancelled_at: string | null
  items: OrderItem[]
}

export interface OrderItemInput {
  sku: string
  qty: number
}

export interface OrderInput {
  customer: string
  items: OrderItemInput[]
}

export interface DailySales {
  date: string
  order_count: number
  cancelled_count: number
  units_sold: number
  revenue: number
}

export interface FieldError {
  field: string
  message: string
}

export interface ApiErrorBody {
  code: string
  message: string
  sku?: string
  available?: number
  details?: FieldError[]
}

export interface ProductInput {
  sku: string
  name: string
  price: number
  is_active: boolean
  min_stock: number
}

export type AuditEntity = 'product' | 'order'

export interface AuditEntry {
  id: number
  entity_type: AuditEntity
  entity_id: number
  entity_label: string
  action: string
  details: Record<string, unknown>
  created_at: string
}
