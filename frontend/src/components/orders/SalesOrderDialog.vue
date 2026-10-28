<template>
  <el-dialog :model-value="modelValue" :title="order ? `修改訂單 ${order.soNo}` : '新增客戶訂單'" width="min(760px, 96vw)"
    @update:model-value="emit('update:modelValue', $event)">
    <el-form label-width="80px" @submit.prevent>
      <el-form-item label="客戶" required>
        <el-select v-model="form.customerId" filterable placeholder="選擇客戶" class="w-cust">
          <el-option v-for="c in customers.filter((x) => x.isActive)" :key="c.id" :label="`${c.code} ${c.name}`" :value="c.id" />
        </el-select>
        <span v-if="!customers.length" class="muted hint">請先到「客戶」分頁新增客戶</span>
      </el-form-item>
      <el-form-item label="客戶 PO"><el-input v-model="form.customerPo" maxlength="50" class="w-cust" /></el-form-item>
      <el-form-item label="備註"><el-input v-model="form.note" /></el-form-item>
    </el-form>

    <el-table :data="form.lines" size="small" empty-text="請加入訂購品項">
      <el-table-column label="#" width="40"><template #default="{ $index }">{{ $index + 1 }}</template></el-table-column>
      <el-table-column label="品項（成品）" min-width="220">
        <template #default="{ row }">
          <el-select v-model="row.productId" filterable placeholder="搜尋品號或品名" class="w-full">
            <el-option v-for="p in fgProducts" :key="p.id" :label="`${p.itemNo} ${p.name}`" :value="p.id" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column label="數量" width="150">
        <template #default="{ row }"><el-input-number v-model="row.qty" :min="1" :step="100" controls-position="right" class="w-full" /></template>
      </el-table-column>
      <el-table-column label="交期" width="160">
        <template #default="{ row }">
          <el-date-picker v-model="row.dueDate" type="date" value-format="YYYY-MM-DD" :clearable="false" style="--el-date-editor-width: 140px" />
        </template>
      </el-table-column>
      <el-table-column width="48">
        <template #default="{ $index }"><el-button size="small" text type="danger" aria-label="移除" @click="form.lines.splice($index, 1)">✕</el-button></template>
      </el-table-column>
    </el-table>
    <el-button size="small" class="add" @click="addLine">＋ 加品項</el-button>

    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">{{ order ? '儲存' : '建立訂單' }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../../api'
import type { Customer, ProductLite, SalesOrder } from './types'

const props = defineProps<{ modelValue: boolean; order: SalesOrder | null; customers: Customer[]; products: ProductLite[] }>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; saved: [] }>()
const saving = ref(false)
const fgProducts = computed(() => props.products.filter((p) => p.category === 'FG' && p.isActive))
const defaultDue = () => new Date(Date.now() + 14 * 86400_000).toISOString().slice(0, 10)
const blank = () => ({ customerId: '', customerPo: '', note: '', lines: [] as { productId: string; qty: number; dueDate: string }[] })
const form = ref(blank())

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return
    const o = props.order
    form.value = o
      ? {
          customerId: o.customerId, customerPo: o.customerPo ?? '', note: o.note ?? '',
          lines: o.lines.map((l) => ({ productId: l.productId, qty: l.qty, dueDate: l.dueDate.slice(0, 10) })),
        }
      : { ...blank(), lines: [{ productId: '', qty: 100, dueDate: defaultDue() }] }
  },
)

function addLine() {
  const last = form.value.lines.at(-1)
  form.value.lines.push({ productId: '', qty: 100, dueDate: last?.dueDate ?? defaultDue() })
}

async function save() {
  const lines = form.value.lines.filter((l) => l.productId && l.qty > 0)
  if (!form.value.customerId) return ElMessage.error('請選擇客戶')
  if (!lines.length) return ElMessage.error('請至少加入一個品項')
  saving.value = true
  try {
    const body = { ...form.value, lines }
    if (props.order) await api(`/sales-orders/${props.order.id}`, { method: 'PUT', body })
    else {
      const res = await api<{ soNo: string }>('/sales-orders', { method: 'POST', body })
      ElMessage.success(`已建立訂單 ${res.soNo}`)
    }
    emit('saved')
    emit('update:modelValue', false)
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.w-cust { width: min(320px, 100%); }
.w-full { width: 100%; }
.hint { margin-left: 10px; font-size: 12px; }
.add { margin-top: 8px; }
</style>
