<template>
  <div v-if="items.length" class="bars" role="list">
    <div v-for="it in items" :key="it.label" class="row" role="listitem"
      @mouseenter="showTip($event, it.label, it.tip)" @mousemove="showTip($event, it.label, it.tip)" @mouseleave="hideTip">
      <div class="lbl">
        <span>{{ it.label }}</span><span v-if="it.sub" class="muted sub">{{ it.sub }}</span>
      </div>
      <div class="track">
        <div class="fill" :style="{ width: `calc((100% - 64px) * ${max ? it.value / max : 0})` }" />
        <span class="val num">{{ fmt.int(it.value) }}<small class="muted"> {{ unit }}</small></span>
      </div>
    </div>
  </div>
  <el-empty v-else :image-size="48" :description="empty" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { fmt } from '../../api'
import { hideTip, showTip, type TipRow } from '../tooltip'

const props = defineProps<{
  items: { label: string; sub?: string; value: number; tip: TipRow[] }[]
  unit: string
  empty: string
}>()
const max = computed(() => Math.max(0, ...props.items.map((i) => i.value)))
</script>

<style scoped>
.bars { display: flex; flex-direction: column; gap: 10px; }
.row { display: grid; grid-template-columns: minmax(96px, 38%) 1fr; gap: 10px; align-items: center; cursor: default; }
.lbl { font-size: 13px; display: flex; flex-direction: column; line-height: 1.3; min-width: 0; }
.lbl span:first-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sub { font-size: 11px; }
.track { display: flex; align-items: center; gap: 8px; min-width: 0; }
.fill { height: 14px; background: var(--series-1); border-radius: 0 4px 4px 0; min-width: 2px; flex: none; }
.val { font-size: 13px; white-space: nowrap; }
.val small { font-size: 11px; }
.row:hover .fill { filter: brightness(1.1); }
</style>
