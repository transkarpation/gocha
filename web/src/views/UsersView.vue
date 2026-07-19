<script setup lang="ts">
import { onMounted, ref } from 'vue'

import {
  deleteUser,
  listUsers,
  restoreUser,
  updateUser,
  type UpdateUserPayload,
} from '@/services/users.service'
import { errorMessage } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import type { Role, User } from '@/types'

const auth = useAuthStore()

const PAGE = 20

const users = ref<User[]>([])
const offset = ref(0)
const hasMore = ref(false)
const loading = ref(false)
const error = ref('')
const notice = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const { data } = await listUsers({ limit: PAGE, offset: offset.value })
    users.value = data.users
    hasMore.value = data.users.length === PAGE
  } catch (e) {
    error.value = errorMessage(e, 'Could not load users')
  } finally {
    loading.value = false
  }
}

function next() {
  offset.value += PAGE
  load()
}

function prev() {
  offset.value = Math.max(0, offset.value - PAGE)
  load()
}

// --- Editing ---
const editing = ref<User | null>(null)
const editEmail = ref('')
const editDisplayName = ref('')
const editRole = ref<Role>('user')
const editPassword = ref('')
const saving = ref(false)

function startEdit(u: User) {
  editing.value = u
  editEmail.value = u.email
  editDisplayName.value = u.display_name
  editRole.value = u.role
  editPassword.value = ''
  error.value = ''
  notice.value = ''
}

function cancelEdit() {
  editing.value = null
}

async function saveEdit() {
  if (!editing.value) return
  const original = editing.value
  const payload: UpdateUserPayload = {}
  if (editEmail.value !== original.email) payload.email = editEmail.value
  // Sent even when blanked out — that clears the name server-side.
  if (editDisplayName.value.trim() !== original.display_name) {
    payload.display_name = editDisplayName.value.trim()
  }
  if (editRole.value !== original.role) payload.role = editRole.value
  if (editPassword.value) payload.password = editPassword.value

  if (Object.keys(payload).length === 0) {
    error.value = 'Nothing changed.'
    return
  }

  saving.value = true
  error.value = ''
  try {
    await updateUser(original.id, payload)
    notice.value = `Updated ${editEmail.value}.` + (payload.password ? ' Their sessions were revoked.' : '')
    editing.value = null
    await load()
  } catch (e) {
    error.value = errorMessage(e, 'Could not update user')
  } finally {
    saving.value = false
  }
}

// --- Delete (soft) ---
const lastDeletedId = ref('')

async function remove(u: User) {
  if (u.id === auth.user?.id && !confirm('This is your own account. Delete it anyway?')) return
  if (!confirm(`Soft-delete ${u.email}? They can be restored by ID.`)) return
  error.value = ''
  notice.value = ''
  try {
    await deleteUser(u.id)
    lastDeletedId.value = u.id
    notice.value = `Soft-deleted ${u.email}.`
    await load()
  } catch (e) {
    error.value = errorMessage(e, 'Could not delete user')
  }
}

// --- Restore ---
const restoreId = ref('')

async function restore(id: string) {
  const target = id.trim()
  if (!target) return
  error.value = ''
  notice.value = ''
  try {
    const { data } = await restoreUser(target)
    notice.value = `Restored ${data.email}.`
    restoreId.value = ''
    if (lastDeletedId.value === target) lastDeletedId.value = ''
    await load()
  } catch (e) {
    error.value = errorMessage(e, 'Could not restore user')
  }
}

function formatDate(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

onMounted(load)
</script>

<template>
  <div class="row-between" style="margin-bottom: 1.5rem">
    <h1 style="margin: 0">Users</h1>
    <button class="btn btn-sm" :disabled="loading" @click="load">Refresh</button>
  </div>

  <div v-if="error" class="alert alert-error">{{ error }}</div>
  <div v-if="notice" class="alert alert-success">{{ notice }}</div>

  <div v-if="lastDeletedId" class="alert alert-success">
    Just deleted
    <span class="mono">{{ lastDeletedId }}</span>
    <button class="btn btn-sm" style="margin-left: 0.75rem" @click="restore(lastDeletedId)">
      Undo (restore)
    </button>
  </div>

  <div class="card">
    <div class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Email</th>
            <th>Role</th>
            <th>Created</th>
            <th>ID</th>
            <th style="width: 1%"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in users" :key="u.id">
            <td>
              <span v-if="u.display_name">{{ u.display_name }}</span>
              <span v-else class="muted small">—</span>
            </td>
            <td>{{ u.email }}</td>
            <td><span class="badge" :class="{ public: u.role === 'admin' }">{{ u.role }}</span></td>
            <td class="small muted">{{ formatDate(u.created_at) }}</td>
            <td class="mono small">{{ u.id }}</td>
            <td>
              <div class="row" style="gap: 0.4rem; flex-wrap: nowrap">
                <button class="btn btn-sm" @click="startEdit(u)">Edit</button>
                <button class="btn btn-sm btn-danger" @click="remove(u)">Delete</button>
              </div>
            </td>
          </tr>
          <tr v-if="!users.length && !loading">
            <td colspan="5" class="empty">No users on this page.</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="row-between" style="margin-top: 1rem">
      <span class="muted small">
        Showing {{ offset + 1 }}–{{ offset + users.length }}
        <span class="muted"> · soft-deleted users are hidden</span>
      </span>
      <div class="row">
        <button class="btn btn-sm" :disabled="offset === 0 || loading" @click="prev">Previous</button>
        <button class="btn btn-sm" :disabled="!hasMore || loading" @click="next">Next</button>
      </div>
    </div>
  </div>

  <div v-if="editing" class="card">
    <h2>Edit user</h2>
    <p class="muted small mono">{{ editing.id }}</p>
    <div class="field">
      <label for="edit-email">Email</label>
      <input id="edit-email" v-model="editEmail" type="email" />
    </div>
    <div class="field">
      <label for="edit-display-name">Display name</label>
      <input id="edit-display-name" v-model="editDisplayName" type="text" maxlength="64" />
      <span class="muted small">Leave empty to clear it.</span>
    </div>
    <div class="field">
      <label for="edit-role">Role</label>
      <select id="edit-role" v-model="editRole">
        <option value="user">user</option>
        <option value="admin">admin</option>
      </select>
    </div>
    <div class="field">
      <label for="edit-password">New password (optional)</label>
      <input
        id="edit-password"
        v-model="editPassword"
        type="password"
        autocomplete="new-password"
        placeholder="leave blank to keep current"
      />
      <span class="muted small">Setting a password revokes all of that user's tokens.</span>
    </div>
    <div class="row">
      <button class="btn btn-primary" :disabled="saving" @click="saveEdit">
        {{ saving ? 'Saving…' : 'Save changes' }}
      </button>
      <button class="btn" :disabled="saving" @click="cancelEdit">Cancel</button>
    </div>
  </div>

  <div class="card">
    <h2>Restore a soft-deleted user</h2>
    <p class="muted small">
      Deleted users don't appear in the list above; restore them by their ID.
    </p>
    <form class="row" @submit.prevent="restore(restoreId)">
      <input v-model="restoreId" class="mono" placeholder="user ID" style="flex: 1; min-width: 220px" />
      <button class="btn" :disabled="!restoreId.trim()">Restore</button>
    </form>
  </div>
</template>
