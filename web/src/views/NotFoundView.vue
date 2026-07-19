<script setup lang="ts">
import { RouterLink } from 'vue-router'
</script>

<template>
  <div class="empty">
    <h1>404</h1>
    <p class="muted">This page doesn't exist.</p>
    <RouterLink class="btn" :to="{ name: 'home' }">Back home</RouterLink>
  </div>
</template>
