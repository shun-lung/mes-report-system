<template>
  <div class="public">
    <Dashboard public-view />
    <p class="muted small foot">報工系統（MES）展示 · Go＋PostgreSQL＋Vue · 資料為測試用</p>
  </div>
</template>

<script setup lang="ts">
import Dashboard from './Dashboard.vue'
</script>

<style scoped>
.public { min-height: 100vh; background: var(--bg, transparent); }
.foot { text-align: center; padding: 0 16px 24px; }
.small { font-size: 12px; }
</style>
