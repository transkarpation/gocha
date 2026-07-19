<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'

import { useAuthStore } from '@/stores/auth'
import { errorMessage, fieldErrors } from '@/api/client'

const auth = useAuthStore()
const router = useRouter()

const email = ref('')
const displayName = ref('')
const password = ref('')
const error = ref('')
// Per-field messages from a 422, keyed as the API spells the field.
const fields = ref<Record<string, string>>({})
const loading = ref(false)

// The server rejects passwords shorter than 8 characters (422); mirror that
// here so the user gets immediate feedback instead of a round trip.
const MIN_PASSWORD = 8

// The API treats display_name as optional (the CLI and older accounts have
// none), but a self-registering user should introduce themselves, so the
// form requires it. The server caps it at 64 characters.
const MAX_DISPLAY_NAME = 64

async function onSubmit() {
  error.value = ''
  fields.value = {}
  if (!displayName.value.trim()) {
    error.value = 'Display name is required'
    return
  }
  if (password.value.length < MIN_PASSWORD) {
    error.value = `Password must be at least ${MIN_PASSWORD} characters`
    return
  }
  loading.value = true
  try {
    await auth.register(email.value, password.value, displayName.value.trim())
    router.push({ name: 'home' })
  } catch (e) {
    error.value = errorMessage(e, 'Registration failed')
    fields.value = fieldErrors(e)
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
      <h2 class="mt-4 text-3xl font-bold tracking-tight text-white">Create your account</h2>
      <p class="mt-2 text-sm text-gray-400">
        Already have an account?
        <RouterLink :to="{ name: 'login' }" class="font-semibold text-indigo-400 hover:text-indigo-300">
          Sign in
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
          <label for="display-name" class="block text-sm font-semibold text-white">
            Display name
          </label>
          <input
            id="display-name"
            v-model="displayName"
            type="text"
            autocomplete="name"
            required
            :maxlength="MAX_DISPLAY_NAME"
            placeholder="Alice Liddell"
            class="mt-2 block w-full rounded-md bg-white/5 px-3 py-2 text-base text-white outline-1 -outline-offset-1 outline-white/10 placeholder:text-gray-500 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-500"
          />
          <p v-if="fields.display_name" class="mt-2 text-sm text-red-400">
            Display name {{ fields.display_name }}
          </p>
        </div>

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
          <p v-if="fields.email" class="mt-2 text-sm text-red-400">Email {{ fields.email }}</p>
        </div>

        <div>
          <label for="password" class="block text-sm font-semibold text-white">Password</label>
          <input
            id="password"
            v-model="password"
            type="password"
            autocomplete="new-password"
            :minlength="MIN_PASSWORD"
            required
            placeholder="••••••••"
            class="mt-2 block w-full rounded-md bg-white/5 px-3 py-2 text-base text-white outline-1 -outline-offset-1 outline-white/10 placeholder:text-gray-500 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-500"
          />
          <p v-if="fields.password" class="mt-2 text-sm text-red-400">
            Password {{ fields.password }}
          </p>
          <p v-else class="mt-2 text-sm text-gray-400">At least {{ MIN_PASSWORD }} characters.</p>
        </div>

        <button
          type="submit"
          :disabled="loading"
          class="w-full rounded-md bg-indigo-600 px-3 py-2.5 text-sm font-semibold text-white hover:bg-indigo-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-500 disabled:opacity-60"
        >
          {{ loading ? 'Creating…' : 'Sign up' }}
        </button>

        <p class="text-center text-sm text-gray-500">
          New accounts are always created with the <strong class="text-gray-400">user</strong> role.
        </p>
      </form>
    </div>
  </div>
</template>
