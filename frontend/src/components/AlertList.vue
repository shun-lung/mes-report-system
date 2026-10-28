<template>
  <el-table :data="alerts" size="small" empty-text="沒有預警">
    <el-table-column label="料件" min-width="170">
      <template #default="{ row }">
        <b>{{ row.itemNo }}</b> {{ row.itemName }}
        <div class="muted">{{ catName[row.category] }}</div>
      </template>
    </el-table-column>
    <el-table-column label="製令" prop="woNo" min-width="170" />
    <el-table-column label="需求" align="right" width="90">
      <template #default="{ row }"><span class="num">{{ qty(row.requiredQty) }}</span> {{ row.unit }}</template>
    </el-table-column>
    <el-table-column label="可用" align="right" width="90">
      <template #default="{ row }"><span class="num short">{{ qty(row.availableQty) }}</span></template>
    </el-table-column>
    <el-table-column label="缺口" align="right" width="80">
      <template #default="{ row }"><span class="num">{{ qty(row.shortageQty) }}</span></template>
    </el-table-column>
    <el-table-column label="處理" min-width="200">
      <template #default="{ row }">
        <template v-if="row.status === 'PENDING'">
          <el-button v-if="row.canCreateWo" size="small" type="primary" :disabled="!canDecide" :loading="busy === row.id"
            @click="decide(row, 'CREATE_WO')">通知建單</el-button>
          <el-button v-else size="small" type="primary" :disabled="!canDecide" :loading="busy === row.id"
            @click="decide(row, 'CREATE_PO')">建立採購單</el-button>
          <el-button size="small" :disabled="!canDecide" @click="decide(row, 'PASS')">Pass</el-button>
        </template>
        <template v-else>
          <el-tag :type="row.status === 'PASSED' ? 'info' : 'success'" size="small">{{ decidedText(row) }}</el-tag>
          <span class="muted by">{{ row.decidedBy }}</span>
        </template>
      </template>
    </el-table-column>
  </el-table>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, fmt } from '../api'
import { useSession } from '../stores/session'

export interface Alert {
  id: string
  status: string
  woNo: string | null
  itemNo: string
  itemName: string
  category: string
  unit: string
  requiredQty: number
  availableQty: number
  shortageQty: number
  canCreateWo: boolean
  createdWoNo: string | null
  createdPoNo: string | null
  decidedBy: string | null
}

defineProps<{ alerts: Alert[] }>()
const emit = defineEmits<{ decided: [] }>()
const session = useSession()
const canDecide = computed(() => session.can('alert:decide'))
const busy = ref('')
const catName: Record<string, string> = { FG: '成品', SFG: '半成品', RAW: '基本材料', AUX: '輔助料件' }
const qty = (v: number) => fmt.dec(v, v % 1 === 0 ? 0 : 2)

function decidedText(row: Alert) {
  if (row.status === 'CREATED_WO') return '已建製令 ' + row.createdWoNo
  if (row.status === 'CREATED_PO') return '已請購 ' + (row.createdPoNo ?? '')
  return '已 Pass'
}

async function decide(row: Alert, action: 'CREATE_WO' | 'CREATE_PO' | 'PASS') {
  busy.value = row.id
  try {
    const res = await api<{ newWorkOrderNo?: string; newPurchaseOrderNo?: string }>(`/alerts/${row.id}/decide`, { method: 'POST', body: { action } })
    ElMessage.success(
      action === 'PASS' ? '已 Pass'
        : action === 'CREATE_PO' ? `已加入採購單草稿 ${res.newPurchaseOrderNo}，請到「採購單」確認後下單`
        : `已建立半成品製令 ${res.newWorkOrderNo}（草稿）`,
    )
    emit('decided')
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    busy.value = ''
  }
}
</script>

<style scoped>
.short { color: var(--div-neg-ink); font-weight: 600; }
.by { margin-left: 6px; font-size: 12px; }
</style>
