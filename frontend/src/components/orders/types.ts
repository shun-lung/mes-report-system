export interface Customer { id: string; code: string; name: string; isActive: boolean; orders: number }
export interface ProductLite { id: string; itemNo: string; name: string; unit: string; category: string; isActive: boolean; activeRoutingVersion: number | null }
export interface SalesOrderLine {
  productId: string; lineNo: number; itemNo: string; itemName: string; qty: number; dueDate: string
  woId: string | null; woNo: string | null; woStatus: string | null; finishedQty: number
}
export interface SalesOrder {
  id: string; soNo: string; status: string; customerId: string; customer: string; customerPo: string | null
  note: string | null; orderDate: string; lines: SalesOrderLine[]
}
