<template>
  <div v-if="tip.visible" class="chart-tip" :style="style" role="tooltip">
    <div class="tip-title">{{ tip.title }}</div>
    <div v-for="r in tip.rows" :key="r.label" class="tip-row">
      <span v-if="r.swatch" class="sw" :style="{ background: r.swatch }" />
      <span class="lbl">{{ r.label }}</span>
      <span class="val num">{{ r.value }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { tip } from './tooltip'

const style = computed(() => {
  const flip = tip.x > window.innerWidth - 240
  return {
    left: flip ? `${tip.x - 14}px` : `${tip.x + 14}px`,
    top: `${tip.y + 14}px`,
    transform: flip ? 'translateX(-100%)' : 'none',
  }
})
</script>

<style scoped>
.chart-tip {
  position: fixed;
  z-index: 3000;
  pointer-events: none;
  background: var(--surface-1);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 6px 20px rgb(0 0 0 / 0.14);
  padding: 8px 10px;
  min-width: 160px;
  font-size: 12px;
}
.tip-title { font-weight: 600; margin-bottom: 4px; color: var(--text-primary); }
.tip-row { display: flex; align-items: center; gap: 6px; line-height: 1.7; color: var(--text-secondary); }
.tip-row .lbl { flex: 1; }
.tip-row .val { color: var(--text-primary); font-weight: 500; }
.sw { width: 10px; height: 10px; border-radius: 3px; flex: none; }
</style>
