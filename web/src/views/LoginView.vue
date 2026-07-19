<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'

import { useAuthStore } from '@/stores/auth'
import { errorMessage } from '@/api/client'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const email = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

async function onSubmit() {
  error.value = ''
  loading.value = true
  try {
    await auth.login(email.value, password.value)
    const redirect = route.query.redirect
    router.push(typeof redirect === 'string' ? redirect : { name: 'home' })
  } catch (e) {
    error.value = errorMessage(e, 'Login failed')
    password.value = ''
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex min-h-full flex-col justify-center px-6 py-16">
    <div class="mx-auto w-full max-w-sm text-center">
      <p class="text-sm font-bold tracking-widest text-indigo-500 uppercase">Ethora</p>
      <h2 class="mt-4 text-3xl font-bold tracking-tight text-white">Sign in to your account</h2>
      <p class="mt-2 text-sm text-gray-400">
        Don't have an account?
        <RouterLink :to="{ name: 'register' }" class="font-semibold text-indigo-400 hover:text-indigo-300">
          Sign up
        </RouterLink>
      </p>
    </div>

    <div class="mx-auto mt-10 w-full max-w-lg rounded-xl bg-gray-900 p-8 ring-1 ring-white/10">
      <div
        v-if="error"
        class="mb-6 rounded-md bg-red-500/10 px-4 py-3 text-sm text-red-400 ring-1 ring-red-500/30"
      >
        {{ error }}
      </div>

      <form class="space-y-6" @submit.prevent="onSubmit">
        <div>
          <label for="email" class="block text-sm font-semibold text-white">Email address</label>
          <input
            id="email"
            v-model="email"
            type="email"
            autocomplete="email"
            required
            placeholder="you@example.com"
            class="mt-2 block w-full rounded-md bg-white/5 px-3 py-2 text-base text-white outline-1 -outline-offset-1 outline-white/10 placeholder:text-gray-500 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-500"
          />
        </div>

        <div>
          <label for="password" class="block text-sm font-semibold text-white">Password</label>
          <input
            id="password"
            v-model="password"
            type="password"
            autocomplete="current-password"
            required
            placeholder="••••••••"
            class="mt-2 block w-full rounded-md bg-white/5 px-3 py-2 text-base text-white outline-1 -outline-offset-1 outline-white/10 placeholder:text-gray-500 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-500"
          />
        </div>

        <button
          type="submit"
          :disabled="loading"
          class="w-full rounded-md bg-indigo-600 px-3 py-2.5 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-500 disabled:opacity-60"
        >
          {{ loading ? 'Signing in…' : 'Sign in' }}
        </button>
      </form>
    </div>
  </div>
</template>
