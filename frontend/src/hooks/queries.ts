import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type ListProductsParams } from '../api/client'
import type { AuditEntity, OrderInput, ProductInput, StockAdjustment } from '../api/types'

export const keys = {
  products: ['products'] as const,
  summary: ['summary'] as const,
  orders: ['orders'] as const,
  audit: ['audit'] as const,
  order: (id: number) => ['order', id] as const,
}

export const useProducts = (p: ListProductsParams) =>
  useQuery({
    queryKey: [...keys.products, p],
    queryFn: ({ signal }) => api.listProducts(p, signal),
    placeholderData: keepPreviousData,
  })

export const useSummary = () =>
  useQuery({ queryKey: [...keys.summary, 'stock'], queryFn: ({ signal }) => api.summary(signal) })

export const useDailySales = () =>
  useQuery({ queryKey: [...keys.summary, 'daily'], queryFn: ({ signal }) => api.dailySales(signal) })

export const useOrders = () =>
  useQuery({ queryKey: keys.orders, queryFn: ({ signal }) => api.listOrders(signal).then((r) => r.data) })

export const useMovements = (id: number) =>
  useQuery({ queryKey: ['movements', id], queryFn: () => api.listMovements(id).then((r) => r.data) })

export const useOrder = (id: number) => useQuery({ queryKey: keys.order(id), queryFn: () => api.getOrder(id) })

function useInvalidateStock() {
  const qc = useQueryClient()
  return () =>
    Promise.all([keys.products, keys.summary, keys.orders, keys.audit].map((queryKey) => qc.invalidateQueries({ queryKey })))
}

export function useAdjustStock() {
  const invalidate = useInvalidateStock()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { id: number; adj: StockAdjustment }) => api.adjustStock(v.id, v.adj),
    onSuccess: (_r, v) => {
      void qc.invalidateQueries({ queryKey: ['movements', v.id] })
      return invalidate()
    },
  })
}

export function useCreateOrder() {
  const invalidate = useInvalidateStock()
  return useMutation({
    mutationFn: (v: { input: OrderInput; key: string }) => api.createOrder(v.input, v.key),
    onSuccess: invalidate,
  })
}

export function useCancelOrder() {
  const invalidate = useInvalidateStock()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.cancelOrder(id),
    onSuccess: (order) => {
      qc.setQueryData(keys.order(order.id), order)
      return invalidate()
    },
    onError: (_e, id) => qc.invalidateQueries({ queryKey: keys.order(id) }),
  })
}

export const useAudit = (p: { entity: AuditEntity | ''; page: number; limit: number }) =>
  useQuery({
    queryKey: [...keys.audit, p],
    queryFn: ({ signal }) => api.listAudit(p, signal),
    placeholderData: keepPreviousData,
  })

export function useCreateProduct() {
  const invalidate = useInvalidateStock()
  return useMutation({ mutationFn: (input: ProductInput) => api.createProduct(input), onSuccess: invalidate })
}

export function useUpdateProduct() {
  const invalidate = useInvalidateStock()
  return useMutation({
    mutationFn: (v: { id: number; input: ProductInput }) => api.updateProduct(v.id, v.input),
    onSuccess: invalidate,
  })
}

export function useDeleteProduct() {
  const invalidate = useInvalidateStock()
  return useMutation({ mutationFn: (id: number) => api.deleteProduct(id), onSuccess: invalidate })
}
