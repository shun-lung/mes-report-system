<template>
  <div class="page">
    <div class="page-title">
      <h2>產品資料</h2>
      <div class="spacer" />
      <el-input v-model="q" placeholder="品號、品名或條碼" clearable class="search" @input="debouncedLoad" />
      <el-button v-if="canEdit" type="primary" @click="open(null)">新增產品</el-button>
    </div>

    <el-radio-group v-model="category" size="small" @change="load">
      <el-radio-button value="">全部</el-radio-button>
      <el-radio-button v-for="(v, k) in categoryName" :key="k" :value="k">{{ v }}</el-radio-button>
    </el-radio-group>

    <div class="card">
      <el-table :data="products" size="small" class="clickable" @row-click="(r: Product) => open(r.id)">
        <el-table-column width="64">
          <template #default="{ row }">
            <img v-if="row.primaryImageId" :src="`/api/images/${row.primaryImageId}`" class="thumb" alt="" loading="lazy" />
            <div v-else class="thumb empty" />
          </template>
        </el-table-column>
        <el-table-column label="品號" prop="itemNo" width="110" />
        <el-table-column label="品名" min-width="180">
          <template #default="{ row }">{{ row.name }}<div v-if="row.spec" class="muted small ellipsis">{{ row.spec }}</div></template>
        </el-table-column>
        <el-table-column label="類別" width="96">
          <template #default="{ row }"><el-tag size="small" :type="catTag[row.category]">{{ categoryName[row.category] }}</el-tag></template>
        </el-table-column>
        <el-table-column label="條碼" prop="barcode" width="140" />
        <el-table-column label="單位" prop="unit" width="64" />
        <el-table-column label="扣帳" width="96">
          <template #default="{ row }">{{ timingName[row.defaultDeductTiming] }}</template>
        </el-table-column>
        <el-table-column label="BOM／途程" width="120">
          <template #default="{ row }">
            <template v-if="row.category === 'FG' || row.category === 'SFG'">
              <span :class="{ warn: !row.bomLines }">{{ row.bomLines }} 項</span> ·
              <span :class="{ warn: !row.activeRoutingVersion }">{{ row.activeRoutingVersion ? 'v' + row.activeRoutingVersion : '無途程' }}</span>
            </template>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="狀態" width="80">
          <template #default="{ row }">
            <el-tag size="small" :type="row.isActive ? 'success' : 'info'">{{ row.isActive ? '使用中' : '停用' }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <ProductDrawer v-model="drawerOpen" :product-id="currentId" :products="allProducts" :stations="stations" @changed="load" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '../../api'
import { categoryName, timingName } from '../../labels'
import { useSession } from '../../stores/session'
import ProductDrawer from '../../components/master/ProductDrawer.vue'
import type { Product, Station } from '../../components/master/types'

const session = useSession()
const canEdit = computed(() => session.can('product:edit'))
const q = ref('')
const category = ref('')
const products = ref<Product[]>([])
const allProducts = ref<Product[]>([])
const stations = ref<Station[]>([])
const drawerOpen = ref(false)
const currentId = ref<string | null>(null)
const catTag: Record<string, string> = { FG: 'primary', SFG: 'warning', RAW: 'info', AUX: 'info' }
let timer: number | undefined

async function load() {
  const params = new URLSearchParams({ q: q.value, category: category.value })
  ;[products.value, allProducts.value] = await Promise.all([api<Product[]>('/products?' + params), api<Product[]>('/products')])
}
function debouncedLoad() {
  window.clearTimeout(timer)
  timer = window.setTimeout(load, 250)
}
function open(id: string | null) {
  currentId.value = id
  drawerOpen.value = true
}
onMounted(async () => {
  stations.value = await api<Station[]>('/stations').catch(() => [])
  await load()
})
</script>

<style scoped>
.spacer { flex: 1; }
.search { width: 240px; }
.small { font-size: 12px; }
.ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 320px; }
.thumb { width: 40px; height: 40px; border-radius: 6px; object-fit: cover; display: block; background: var(--surface-2); }
.thumb.empty { border: 1px dashed var(--border); }
.warn { color: var(--div-neg-ink); }
.clickable :deep(tr) { cursor: pointer; }
@media (max-width: 760px) { .search { width: 100%; } }
</style>
