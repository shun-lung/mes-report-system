<template>
  <div>
    <div class="grid">
      <figure v-for="img in images" :key="img.id" class="img" :class="{ primary: img.isPrimary }">
        <a :href="`/api/images/${img.id}`" target="_blank" rel="noopener"><img :src="`/api/images/${img.id}`" :alt="img.fileName" /></a>
        <figcaption>
          <el-tag v-if="img.isPrimary" size="small" type="primary">主圖</el-tag>
          <span class="name" :title="img.fileName">{{ img.fileName }}</span>
        </figcaption>
        <div v-if="canEdit" class="actions">
          <el-button v-if="!img.isPrimary" size="small" text @click="setPrimary(img.id)">設為主圖</el-button>
          <el-popconfirm title="刪除這張圖片？" @confirm="remove(img.id)">
            <template #reference><el-button size="small" text type="danger">刪除</el-button></template>
          </el-popconfirm>
        </div>
      </figure>
      <label v-if="canEdit" class="img add" :class="{ busy: uploading }">
        <input type="file" accept="image/jpeg,image/png,image/webp,image/gif" multiple hidden @change="onPick" />
        <span>{{ uploading ? '上傳中…' : '＋ 上傳圖片' }}</span>
        <small class="muted">JPG／PNG／WebP，每張 ≤ 2 MB</small>
      </label>
    </div>
    <el-empty v-if="!images.length && !canEdit" description="尚無圖片" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, upload } from '../../api'

export interface Image { id: string; fileName: string; mimeType: string; fileSize: number; isPrimary: boolean }

const props = defineProps<{ productId: string; images: Image[]; canEdit: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const uploading = ref(false)

async function onPick(e: Event) {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  uploading.value = true
  for (const f of files) {
    if (f.size > 2 * 1024 * 1024) {
      ElMessage.error(`${f.name} 超過 2 MB`)
      continue
    }
    try {
      await upload(`/products/${props.productId}/images`, f)
    } catch (err) {
      ElMessage.error(`${f.name}：${(err as Error).message}`)
    }
  }
  uploading.value = false
  emit('changed')
}
async function setPrimary(id: string) {
  await api(`/product-images/${id}/primary`, { method: 'PUT' })
  emit('changed')
}
async function remove(id: string) {
  await api(`/product-images/${id}`, { method: 'DELETE' })
  emit('changed')
}
</script>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 12px; }
.img { margin: 0; border: 1px solid var(--border); border-radius: 8px; overflow: hidden; background: var(--surface-1); display: flex; flex-direction: column; }
.img.primary { border-color: var(--series-1); }
.img img { width: 100%; aspect-ratio: 1; object-fit: cover; display: block; background: var(--surface-2); }
figcaption { display: flex; gap: 6px; align-items: center; padding: 6px 8px 0; font-size: 12px; min-width: 0; }
.name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-secondary); }
.actions { display: flex; justify-content: space-between; padding: 0 4px 4px; }
.add { aspect-ratio: 1; align-items: center; justify-content: center; gap: 6px; border-style: dashed; cursor: pointer; color: var(--text-secondary); text-align: center; padding: 8px; }
.add:hover { border-color: var(--series-1); color: var(--text-primary); }
.add.busy { pointer-events: none; opacity: 0.6; }
.add small { font-size: 11px; }
</style>
