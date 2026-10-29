<template>
  <el-dialog :model-value="modelValue" :fullscreen="full" width="min(1240px, 98vw)" top="2vh" destroy-on-close
    class="scan-dialog" :show-close="false" @update:model-value="(v: boolean) => emit('update:modelValue', v)" @closed="onClosed">
    <template #header>
      <div class="scan-head">
        <b>{{ preset?.length ? `現場報數（${preset.length} 張製令）` : '現場掃描' }}</b>
        <span class="spacer" />
        <el-button size="small" @click="toggleFull">{{ full ? '離開全螢幕' : '全螢幕' }}</el-button>
        <el-button size="small" @click="emit('update:modelValue', false)">關閉</el-button>
      </div>
    </template>
    <StationPanel embedded :preset="preset" />
  </el-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import StationPanel from './StationPanel.vue'
import type { PresetWO } from './types'

defineProps<{ modelValue: boolean; preset?: PresetWO[] }>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; closed: [] }>()
const full = ref(false)

// 全螢幕：對話框鋪滿，並在支援的裝置（平板）切換瀏覽器全螢幕
async function toggleFull() {
  full.value = !full.value
  try {
    if (full.value && !document.fullscreenElement) await document.documentElement.requestFullscreen()
    else if (!full.value && document.fullscreenElement) await document.exitFullscreen()
  } catch {
    /* 不支援全螢幕時只鋪滿對話框 */
  }
}
function onClosed() {
  full.value = false
  if (document.fullscreenElement) document.exitFullscreen().catch(() => {})
  emit('closed')
}
</script>

<style scoped>
.scan-head { display: flex; align-items: center; gap: 8px; }
.spacer { flex: 1; }
:global(.scan-dialog .el-dialog__body) { padding: 0; }
:global(.scan-dialog .el-dialog__header) { padding-bottom: 8px; }
</style>
