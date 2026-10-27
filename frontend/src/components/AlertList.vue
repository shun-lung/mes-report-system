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
          <el-tooltip :disabled="row.canCreateWo" content="此料件沒有途程，請通知採購；可先 Pass" placement="top">
            <span>
              <el-button size="small" type="primary" :disabled="!row.canCreateWo || !canDecide" :loading="busy === row.id"
                @click="decide(row, 'CREATE_WO')">通知建單</el-button>
            </span>
          </el-tooltip>
          <el-button size="small" :disabled="!canDecide" @click="decide(row, 'PASS')">Pass</el-button>
        </template>
        <template v-else>
          <el-tag :type="row.status === 'CREATED_WO' ? 'success' : 'info'" size="small">
            {{ row.status === 'CREATED_WO' ? '已建單 ' + row.createdWoNo : '已 Pass' }}
          </el-tag>
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
  decidedBy: string | null
}

defineProps<{ alerts: Alert[] }>()
const emit = defineEmits<{ decided: [] }>()
const session = useSession()
const canDecide = computed(() => session.can('alert:decide'))
const busy = ref('')
const catName: Record<string, string> = { FG: '成品', SFG: '半成品', RAW: '基本材料', AUX: '輔助料件' }
const qty = (v: number) => fmt.dec(v, v % 1 === 0 ? 0 : 2)

async function decide(row: Alert, action: 'CREATE_WO' | 'PASS') {
  busy.value = row.id
  try {
    const res = await api<{ newWorkOrderNo?: string }>(`/alerts/${row.id}/decide`, { method: 'POST', body: { action } })
    ElMessage.success(action === 'PASS' ? '已 Pass' : `已建立半成品製令 ${res.newWorkOrderNo}（草稿）`)
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
