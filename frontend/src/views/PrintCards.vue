<template>
  <div class="print-page">
    <div class="toolbar no-print">
      <router-link to="/orders" class="back">← 返回</router-link>
      <div v-if="wo" class="info">
        <b>{{ wo.woNo }}</b> · {{ wo.itemNo }} {{ wo.itemName }} · 共 {{ cards.length }} 張
      </div>
      <span class="spacer" />
      <el-radio-group v-model="scope" size="default">
        <el-radio-button value="all">全部（{{ cards.length }}）</el-radio-button>
        <el-radio-button value="new" :disabled="!unprinted.length">未印過（{{ unprinted.length }}）</el-radio-button>
      </el-radio-group>
      <el-button type="primary" :disabled="!selected.length" @click="doPrint">列印／另存 PDF</el-button>
    </div>
    <p class="hint no-print">A4 直式、每頁 8 張，沿虛線裁切。列印視窗中「目的地」選「另存為 PDF」即可產生 PDF。</p>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" class="no-print" />

    <div v-if="wo" class="sheet">
      <article v-for="(c, i) in selected" :key="c.cardNo" class="fcard">
        <header>
          <span class="factory">{{ wo.factory }} · 流程卡</span>
          <span class="seq">第 {{ indexOf(c) + 1 }} / {{ cards.length }} 張<template v-if="c.printCount"> · 補印</template></span>
        </header>
        <div class="main">
          <div class="left">
            <div class="card-no">{{ c.cardNo }}</div>
            <svg :ref="(el) => setBarcode(el as SVGSVGElement, c.cardNo, i)" class="barcode" />
          </div>
          <img v-if="wo.imageId" :src="`/api/images/${wo.imageId}`" class="pic" alt="" />
        </div>
        <div class="item"><b>{{ wo.itemNo }}</b> {{ wo.itemName }}</div>
        <div v-if="wo.spec" class="spec">{{ wo.spec }}</div>
        <dl class="meta">
          <div><dt>數量</dt><dd class="qty">{{ c.qty }} {{ wo.unit }}</dd></div>
          <div><dt>交期</dt><dd>{{ wo.dueDate?.slice(0, 10) ?? '—' }}</dd></div>
          <div class="wide"><dt>製令</dt><dd>{{ wo.woNo }}<template v-if="wo.customer">　{{ wo.customer }}</template></dd></div>
        </dl>
        <ol class="route">
          <li v-for="(s, j) in wo.routing" :key="j"><span>{{ j + 1 }}. {{ s }}</span><i /></li>
        </ol>
      </article>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import JsBarcode from 'jsbarcode'
import { api } from '../api'
import { useSession } from '../stores/session'

interface Card { cardNo: string; qty: number; status: string; printCount: number; lastPrintedAt: string | null }
interface WO {
  woNo: string; status: string; plannedQty: number; dueDate: string | null; itemNo: string; itemName: string; spec: string | null
  unit: string; imageId: string | null; soNo: string | null; customer: string | null; factory: string; routing: string[]
}

const route = useRoute()
const session = useSession()
const wo = ref<WO | null>(null)
const cards = ref<Card[]>([])
const scope = ref<'all' | 'new'>('all')
const error = ref('')
const unprinted = computed(() => cards.value.filter((c) => !c.printCount))
const selected = computed(() => (scope.value === 'new' ? unprinted.value : cards.value))
const indexOf = (c: Card) => cards.value.indexOf(c)

// Code128 條碼；補上 viewBox 讓 SVG 可依版面縮放（條寬比例不變，掃描不受影響）
function setBarcode(el: SVGSVGElement | null, value: string, _i: number) {
  if (!el || el.dataset.value === value) return
  JsBarcode(el, value, { format: 'CODE128', displayValue: false, height: 50, width: 2, margin: 0 })
  const w = el.getAttribute('width')
  const h = el.getAttribute('height')
  el.setAttribute('viewBox', `0 0 ${parseFloat(w ?? '0')} ${parseFloat(h ?? '0')}`)
  el.setAttribute('preserveAspectRatio', 'none')
  el.removeAttribute('width')
  el.removeAttribute('height')
  el.dataset.value = value
}

async function load() {
  try {
    const res = await api<{ workOrder: WO; cards: Card[] }>(`/work-orders/${route.params.id}/cards`)
    wo.value = res.workOrder
    cards.value = res.cards
    document.title = `流程卡 ${res.workOrder.woNo}`
  } catch (e) {
    error.value = (e as Error).message
  }
}

async function doPrint() {
  const printed = selected.value.map((c) => c.cardNo)
  window.print()
  if (!session.can('wo:release')) return
  try {
    await ElMessageBox.confirm(`是否已成功列印（或存成 PDF）這 ${printed.length} 張流程卡？確認後會記錄列印次數，之後補印會標示「補印」。`, '記錄列印', {
      confirmButtonText: '已列印', cancelButtonText: '沒有列印',
    })
    await api(`/work-orders/${route.params.id}/cards/printed`, { method: 'POST', body: { cardNos: printed } })
    ElMessage.success('已記錄')
    await load()
  } catch {
    /* 使用者選擇沒有列印 */
  }
}

onMounted(load)
</script>

<style scoped>
/* 列印頁固定為白底黑字（紙張），不跟隨暗色模式 */
.print-page { background: #e8e8e5; min-height: 100%; padding: 16px; color: #111; }
.toolbar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; max-width: 210mm; margin: 0 auto; }
.toolbar .info { color: #333; }
.back { color: #333; text-decoration: none; }
.spacer { flex: 1; }
.hint { max-width: 210mm; margin: 8px auto 12px; font-size: 12px; color: #666; }
.sheet {
  width: 210mm; margin: 0 auto; background: #fff; padding: 8mm;
  display: grid; grid-template-columns: 1fr 1fr; grid-auto-rows: 68.5mm; gap: 0;
  box-shadow: 0 2px 12px rgb(0 0 0 / 0.15);
}
.fcard {
  border: 0.3mm dashed #999; padding: 3mm 4mm; display: flex; flex-direction: column; gap: 1.2mm;
  break-inside: avoid; overflow: hidden; font-size: 9pt; line-height: 1.3;
}
.fcard header { display: flex; justify-content: space-between; font-size: 7.5pt; color: #555; }
.main { display: flex; gap: 3mm; align-items: flex-start; }
.left { flex: 1; min-width: 0; }
.card-no { font-size: 11pt; font-weight: 700; letter-spacing: 0.2px; font-variant-numeric: tabular-nums; }
.barcode { display: block; width: 100%; height: 12mm; margin-top: 1mm; }
.pic { width: 16mm; height: 16mm; object-fit: cover; border-radius: 1mm; }
.item { font-size: 10pt; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.spec { font-size: 7.5pt; color: #555; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.meta { display: grid; grid-template-columns: 1fr 1fr; gap: 0.6mm 3mm; margin: 0; }
.meta div { display: flex; gap: 1.5mm; min-width: 0; }
.meta .wide { grid-column: 1 / -1; }
.meta dt { color: #666; flex: none; }
.meta dd { margin: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.meta .qty { font-weight: 700; font-size: 10.5pt; }
.route { list-style: none; margin: auto 0 0; padding: 0; display: flex; gap: 1.5mm; }
.route li { flex: 1; border: 0.25mm solid #333; border-radius: 1mm; font-size: 7.5pt; display: flex; flex-direction: column; min-width: 0; }
.route span { padding: 0.5mm 1mm; background: #f0f0ee; border-bottom: 0.25mm solid #333; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.route i { height: 6mm; }

@media print {
  .no-print { display: none !important; }
  .print-page { background: #fff; padding: 0; }
  .sheet { box-shadow: none; width: auto; padding: 0; }
  .route span { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
}
</style>

<style>
@page { size: A4 portrait; margin: 8mm; }
</style>
