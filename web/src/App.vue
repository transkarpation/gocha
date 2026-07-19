<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

// The guest routes (login/register) are full-bleed pages of their own: no
// navbar, no width-capped `.page` wrapper.
const isAuthPage = computed(() => route.meta.requiresGuest === true)

function logout() {
  auth.logout()
  router.push({ name: 'login' })
}
</script>

<template>
  <div class="app-shell">
    <header v-if="!isAuthPage" class="navbar">
      <RouterLink :to="{ name: 'home' }" class="brand">gocha</RouterLink>

      <nav v-if="auth.isAuthenticated">
        <RouterLink :to="{ name: 'home' }">Home</RouterLink>
        <RouterLink :to="{ name: 'chats' }">Chats</RouterLink>
        <RouterLink v-if="auth.isAdmin" :to="{ name: 'users' }">Users</RouterLink>
      </nav>

      <span class="spacer" />

      <template v-if="auth.isAuthenticated">
        <span class="user-chip">
          {{ auth.user?.display_name || auth.user?.email }}
          <span v-if="auth.user" class="role" :class="{ admin: auth.isAdmin }">
            {{ auth.user.role }}
          </span>
        </span>
        <button class="btn btn-sm" @click="logout">Log out</button>
      </template>
      <nav v-else>
        <RouterLink :to="{ name: 'login' }">Log in</RouterLink>
        <RouterLink :to="{ name: 'register' }">Register</RouterLink>
      </nav>
    </header>

    <main :class="isAuthPage ? 'flex-1' : 'page'">
      <RouterView />
    </main>
  </div>
</template>
