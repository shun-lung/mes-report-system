<template>
  <div class="lines">
    <div class="scan">
      <el-input v-model="scanText" placeholder="掃描料件條碼或輸入品號後按 Enter，可快速加入" clearable @keydown.enter.prevent="onScan" />
    </div>
    <el-table :data="rows" size="small" empty-text="尚未加入料件">
      <el-table-column label="料件" min-width="230">
        <template #default="{ row }">
          <el-select v-model="row.productId" filterable placeholder="搜尋品號或品名" class="w-full">
            <el-option v-for="p in products" :key="p.id" :label="`${p.itemNo} ${p.name}`" :value="p.id"
              :disabled="rows.some((r) => r !== row && r.productId === p.id)" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column :label="mode === 'count' ? '帳面' : '現有'" width="110" align="right">
        <template #default="{ row }">
          <span class="num" :class="{ neg: stock(row.productId) < 0 }">{{ row.productId ? qty(stock(row.productId)) : '—' }}</span>
          <span class="muted"> {{ unit(row.productId) }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="mode === 'count' ? '實盤數量' : '數量'" width="170">
        <template #default="{ row }">
          <el-input-number v-model="row.qty" :min="0" :step="1" controls-position="right" class="w-full" />
        </template>
      </el-table-column>
      <el-table-column v-if="mode === 'count'" label="差異" width="100" align="right">
        <template #default="{ row }">
          <span v-if="row.productId" class="num" :class="diffClass(row)">{{ diffText(row) }}</span>
        </template>
      </el-table-column>
      <el-table-column width="50">
        <template #default="{ $index }">
          <el-button size="small" text type="danger" aria-label="移除" @click="rows.splice($index, 1)">✕</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-button size="small" class="add" @click="rows.push({ productId: '', qty: mode === 'count' ? 0 : 1 })">＋ 加一行</el-button>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { fmt } from '../../api'

export interface Row { productId: string; qty: number }
export interface ProductOpt { id: string; itemNo: string; name: string; unit: string; barcode: string | null; isActive: boolean }

const props = defineProps<{ products: ProductOpt[]; stockOf: (productId: string) => number; mode?: 'qty' | 'count' }>()
const rows = defineModel<Row[]>({ required: true })
const scanText = ref('')

const qty = (v: number) => fmt.dec(v, v % 1 === 0 ? 0 : 2)
const stock = (id: string) => (id ? props.stockOf(id) : 0)
const unit = (id: string) => props.products.find((p) => p.id === id)?.unit ?? ''
const diff = (r: Row) => Math.round((r.qty - stock(r.productId)) * 10000) / 10000
const diffText = (r: Row) => {
  const d = diff(r)
  return d === 0 ? '—' : (d > 0 ? '+' : '') + qty(d)
}
const diffClass = (r: Row) => (diff(r) > 0 ? 'pos' : diff(r) < 0 ? 'neg' : 'muted')

// 掃描條碼或輸入品號：找到就加一行（已存在則數量 +1；盤點模式則帶入帳面數）
function onScan() {
  const key = scanText.value.trim()
  scanText.value = ''
  if (!key) return
  const p = props.products.find((x) => x.barcode === key || x.itemNo.toLowerCase() === key.toLowerCase())
  if (!p) {
    ElMessage.error(`找不到料件「${key}」`)
    return
  }
  const existing = rows.value.find((r) => r.productId === p.id)
  if (existing) {
    if (props.mode !== 'count') existing.qty += 1
  } else {
    const empty = rows.value.find((r) => !r.productId)
    const qtyInit = props.mode === 'count' ? stock(p.id) : 1
    if (empty) Object.assign(empty, { productId: p.id, qty: qtyInit })
    else rows.value.push({ productId: p.id, qty: qtyInit })
  }
}
</script>

<style scoped>
.scan { margin-bottom: 8px; max-width: 420px; }
.w-full { width: 100%; }
.add { margin-top: 8px; }
.neg { color: var(--div-neg-ink); }
.pos { color: var(--div-pos-ink); }
</style>
