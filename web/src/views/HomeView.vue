<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink } from 'vue-router'

import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const showXmppPassword = ref(false)
</script>

<template>
  <div class="row-between" style="margin-bottom: 1.5rem">
    <h1 style="margin: 0">Welcome back</h1>
  </div>

  <div class="card">
    <h2>Your account</h2>
    <div class="table-wrap">
      <table class="table">
        <tbody>
          <tr>
            <th style="width: 160px">User ID</th>
            <td class="mono">{{ auth.user?.id }}</td>
          </tr>
          <tr>
            <th>Email</th>
            <td>{{ auth.user?.email }}</td>
          </tr>
          <tr>
            <th>Role</th>
            <td>
              <span class="badge" :class="{ public: auth.isAdmin }">{{ auth.user?.role }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

  <div class="card">
    <h2>XMPP chat credentials</h2>
    <p class="muted small">
      Handed to you at sign-in for connecting to the Ethora XMPP server directly.
    </p>
    <div v-if="auth.xmpp" class="table-wrap">
      <table class="table">
        <tbody>
          <tr>
            <th style="width: 160px">Username</th>
            <td class="mono">{{ auth.xmpp.username }}</td>
          </tr>
          <tr>
            <th>Password</th>
            <td>
              <span class="mono">{{
                showXmppPassword ? auth.xmpp.password : '•'.repeat(auth.xmpp.password.length)
              }}</span>
              <button
                class="btn btn-sm"
                style="margin-left: 0.75rem"
                @click="showXmppPassword = !showXmppPassword"
              >
                {{ showXmppPassword ? 'Hide' : 'Reveal' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else class="muted">
      No XMPP credentials — chat mirroring is disabled or wasn't set up for this account.
    </p>
  </div>

  <div class="card">
    <h2>Quick links</h2>
    <div class="row">
      <RouterLink class="btn" :to="{ name: 'chats' }">Go to chats</RouterLink>
      <RouterLink v-if="auth.isAdmin" class="btn" :to="{ name: 'users' }">
        Manage users
      </RouterLink>
    </div>
  </div>
</template>
