export interface Product {
  id: string
  itemNo: string
  name: string
  barcode: string | null
  spec: string | null
  unit: string
  category: string
  defaultDeductTiming: string
  safetyStock: number
  alertPct: number
  cardQty: number
  isActive: boolean
  primaryImageId: string | null
  bomLines: number
  activeRoutingVersion: number | null
  defaultSupplierId: string | null
  defaultSupplier: string | null
}
export interface Station { id: string; code: string; name: string; isActive: boolean }
