<template>
  <el-drawer :model-value="modelValue" :size="drawerSize" :title="title" destroy-on-close @update:model-value="emit('update:modelValue', $event)">
    <el-tabs v-model="tab">
      <el-tab-pane label="基本資料" name="basic">
        <el-form label-width="110px" :disabled="!canEdit" @submit.prevent>
          <el-form-item label="品號" required><el-input v-model="form.itemNo" maxlength="50" /></el-form-item>
          <el-form-item label="品名" required><el-input v-model="form.name" maxlength="200" /></el-form-item>
          <el-form-item label="條碼"><el-input v-model="form.barcode" maxlength="100" placeholder="掃描或輸入" /></el-form-item>
          <el-form-item label="規格"><el-input v-model="form.spec" type="textarea" :rows="2" /></el-form-item>
          <el-form-item label="類別" required>
            <el-radio-group v-model="form.category">
              <el-radio-button v-for="(v, k) in categoryName" :key="k" :value="k">{{ v }}</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="單位" required><el-input v-model="form.unit" maxlength="10" class="w-short" /></el-form-item>
          <el-form-item label="預設扣帳時機">
            <el-radio-group v-model="form.defaultDeductTiming">
              <el-radio value="ON_ARRIVAL">到站扣帳</el-radio>
              <el-radio value="ON_ISSUE">出庫扣帳</el-radio>
            </el-radio-group>
            <div class="hint">作為材料被使用時的預設；BOM 每一行可另外指定</div>
          </el-form-item>
          <el-form-item v-if="isMade" label="每張流程卡">
            <el-input-number v-model="form.cardQty" :min="1" /> <span class="unit">{{ form.unit }}</span>
          </el-form-item>
          <el-form-item label="缺料預警緩衝">
            <el-input-number v-model="form.alertPct" :min="0" :max="100" :precision="1" /> <span class="unit">%</span>
            <div class="hint">可用量低於需求 ×（1 + 緩衝%）時預警</div>
          </el-form-item>
          <el-form-item label="安全庫存"><el-input-number v-model="form.safetyStock" :min="0" /> <span class="unit">參考用</span></el-form-item>
          <el-form-item label="使用中"><el-switch v-model="form.isActive" /></el-form-item>
          <el-form-item v-if="canEdit">
            <el-button type="primary" :loading="saving" @click="save">{{ id ? '儲存' : '建立產品' }}</el-button>
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane label="圖片" name="images" :disabled="!id">
        <ImageManager v-if="id" :product-id="id" :images="images" :can-edit="canEdit" @changed="reload" />
      </el-tab-pane>
      <el-tab-pane v-if="isMade" label="BOM" name="bom" :disabled="!id">
        <BomEditor v-if="id && tab === 'bom'" :product-id="id" :products="products" :stations="stations" @changed="emit('changed')" />
      </el-tab-pane>
      <el-tab-pane v-if="isMade" label="途程" name="routing" :disabled="!id">
        <RoutingEditor v-if="id && tab === 'routing'" :product-id="id" :stations="stations" @changed="emit('changed')" />
      </el-tab-pane>
    </el-tabs>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../../api'
import { categoryName } from '../../labels'
import { useSession } from '../../stores/session'
import BomEditor from './BomEditor.vue'
import ImageManager, { type Image } from './ImageManager.vue'
import RoutingEditor from './RoutingEditor.vue'
import type { Product, Station } from './types'

const props = defineProps<{ modelValue: boolean; productId: string | null; products: Product[]; stations: Station[] }>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; changed: [] }>()
const session = useSession()
const canEdit = computed(() => session.can('product:edit'))
const id = ref<string | null>(null)
const tab = ref('basic')
const saving = ref(false)
const images = ref<Image[]>([])
const blank = () => ({
  itemNo: '', name: '', barcode: '', spec: '', unit: '雙', category: 'FG', defaultDeductTiming: 'ON_ISSUE',
  safetyStock: 0, alertPct: 3, cardQty: 100, isActive: true,
})
const form = ref(blank())
const isMade = computed(() => form.value.category === 'FG' || form.value.category === 'SFG')
const title = computed(() => (id.value ? `${form.value.itemNo} ${form.value.name}` : '新增產品'))
const drawerSize = computed(() => (window.innerWidth < 760 ? '100%' : '760px'))

async function reload() {
  if (!id.value) return
  const res = await api<{ product: Product; images: Image[] }>(`/products/${id.value}`)
  const p = res.product
  form.value = {
    itemNo: p.itemNo, name: p.name, barcode: p.barcode ?? '', spec: p.spec ?? '', unit: p.unit, category: p.category,
    defaultDeductTiming: p.defaultDeductTiming, safetyStock: p.safetyStock, alertPct: p.alertPct, cardQty: p.cardQty, isActive: p.isActive,
  }
  images.value = res.images
}

watch(
  () => [props.modelValue, props.productId],
  () => {
    if (!props.modelValue) return
    id.value = props.productId
    tab.value = 'basic'
    images.value = []
    form.value = blank()
    reload()
  },
)

async function save() {
  saving.value = true
  try {
    if (id.value) {
      await api(`/products/${id.value}`, { method: 'PUT', body: form.value })
      ElMessage.success('已儲存')
    } else {
      const res = await api<{ id: string }>('/products', { method: 'POST', body: form.value })
      id.value = res.id
      ElMessage.success(isMade.value ? '已建立，接著可上傳圖片、設定 BOM 與途程' : '已建立')
    }
    emit('changed')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.hint { width: 100%; font-size: 12px; color: var(--text-muted); line-height: 1.5; margin-top: 2px; }
.unit { margin-left: 8px; color: var(--text-muted); }
.w-short { width: 120px; }
</style>
