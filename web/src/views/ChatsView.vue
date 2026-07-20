<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'

import { useAuthStore } from '@/stores/auth'
import { useChatsStore } from '@/stores/chats'
import { createChat, deleteChat } from '@/services/chats.service'
import { userDirectory } from '@/services/users.service'
import { errorMessage } from '@/api/client'
import type { ChatType, DirectoryUser } from '@/types'

const auth = useAuthStore()
const chatsStore = useChatsStore()
const router = useRouter()

const error = ref('')
const notice = ref('')

// --- Who can be added to a chat ---
// GET /users/directory already leaves out the caller and the system account,
// so every entry here is a valid participant. 100 is the server's max page.
const directory = ref<DirectoryUser[]>([])
const directoryError = ref('')
const loadingDirectory = ref(false)
const search = ref('')

function label(u: DirectoryUser) {
  return u.display_name || u.email
}

const matches = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return directory.value
  return directory.value.filter(
    (u) => u.display_name.toLowerCase().includes(q) || u.email.toLowerCase().includes(q),
  )
})

async function loadDirectory() {
  loadingDirectory.value = true
  directoryError.value = ''
  try {
    const { data } = await userDirectory({ limit: 100 })
    directory.value = data.users
  } catch (e) {
    directoryError.value = errorMessage(e, 'Could not load the user list')
  } finally {
    loadingDirectory.value = false
  }
}

onMounted(loadDirectory)

// --- Create a new chat ---
const name = ref('')
const type = ref<ChatType>('public')
const participants = ref<string[]>([])
const creating = ref(false)

const selected = computed(() => directory.value.filter((u) => participants.value.includes(u.id)))

async function onCreate() {
  error.value = ''
  notice.value = ''
  creating.value = true
  try {
    // Both chat types take participants; only a group *requires* one besides
    // the creator, who is added server-side (hence never in the directory).
    const ids = participants.value.length ? participants.value : undefined
    const { data } = await createChat({ name: name.value.trim(), type: type.value, participants: ids })
    chatsStore.remember({ id: data.id, name: data.name, type: data.type })
    notice.value = `Created "${data.name}".`
    name.value = ''
    participants.value = []
    search.value = ''
    router.push({ name: 'chat', params: { id: data.id } })
  } catch (e) {
    error.value = errorMessage(e, 'Could not create chat')
  } finally {
    creating.value = false
  }
}

// --- Open an existing chat by ID ---
const openId = ref('')
const openLabel = ref('')
const openType = ref<ChatType>('public')

function onOpen() {
  error.value = ''
  notice.value = ''
  const id = openId.value.trim()
  if (!id) return
  chatsStore.remember({
    id,
    name: openLabel.value.trim() || `Chat ${id.slice(-6)}`,
    type: openType.value,
  })
  openId.value = ''
  openLabel.value = ''
  router.push({ name: 'chat', params: { id } })
}

// --- Remove from list / delete on server ---
function forget(id: string) {
  chatsStore.forget(id)
}

async function remove(id: string, chatName: string) {
  if (!confirm(`Delete "${chatName}" on the server? This cannot be undone.`)) return
  error.value = ''
  notice.value = ''
  try {
    await deleteChat(id)
    chatsStore.forget(id)
    notice.value = `Deleted "${chatName}".`
  } catch (e) {
    error.value = errorMessage(e, 'Could not delete chat')
  }
}
</script>

<template>
  <div class="row-between" style="margin-bottom: 1.5rem">
    <h1 style="margin: 0">Chats</h1>
  </div>

  <div v-if="error" class="alert alert-error">{{ error }}</div>
  <div v-if="notice" class="alert alert-success">{{ notice }}</div>

  <div class="card">
    <div class="row-between">
      <h2 style="margin: 0">Your chats</h2>
    </div>
    <p class="muted small">
      This list lives in your browser — the server has no endpoint to enumerate chats, so it only
      shows chats you created or opened by ID here.
    </p>

    <div v-if="chatsStore.sorted.length" class="table-wrap" style="margin-top: 1rem">
      <table class="table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Type</th>
            <th>ID</th>
            <th style="width: 1%"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="chat in chatsStore.sorted" :key="chat.id">
            <td>
              <RouterLink :to="{ name: 'chat', params: { id: chat.id } }">{{ chat.name }}</RouterLink>
            </td>
            <td><span class="badge" :class="chat.type">{{ chat.type }}</span></td>
            <td class="mono small">{{ chat.id }}</td>
            <td>
              <div class="row" style="gap: 0.4rem; flex-wrap: nowrap">
                <button class="btn btn-sm" @click="forget(chat.id)">Forget</button>
                <button
                  v-if="auth.isAdmin"
                  class="btn btn-sm btn-danger"
                  @click="remove(chat.id, chat.name)"
                >
                  Delete
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-else class="empty">No chats yet. Create one or open an existing chat by ID below.</div>
  </div>

  <div class="card">
    <h2>Create a chat</h2>
    <form @submit.prevent="onCreate">
      <div class="field">
        <label for="chat-name">Name</label>
        <input id="chat-name" v-model="name" maxlength="100" required />
      </div>
      <div class="field">
        <label for="chat-type">Type</label>
        <select id="chat-type" v-model="type">
          <option value="public">Public — any authenticated user can read</option>
          <option value="group">Group — participants only</option>
        </select>
      </div>
      <div class="field">
        <label>Participants</label>
        <span v-if="type === 'group'" class="muted small">
          You are added automatically; a group needs at least one more. Only
          participants can read a group chat.
        </span>
        <span v-else class="muted small">
          Optional — you are added automatically. A public chat stays readable by
          any signed-in user, so this is its roster, not a restriction.
        </span>

        <div v-if="directoryError" class="alert alert-error" style="margin-top: 0.6rem">
          {{ directoryError }}
          <button type="button" class="btn btn-sm" @click="loadDirectory">Retry</button>
        </div>
        <div v-else-if="loadingDirectory" class="muted small" style="margin-top: 0.6rem">
          Loading users…
        </div>
        <div v-else-if="!directory.length" class="empty" style="margin-top: 0.6rem">
          No other users are registered yet.
        </div>
        <template v-else>
          <input
            v-model="search"
            type="search"
            placeholder="Filter by name or email"
            style="margin-top: 0.6rem"
          />
          <div class="picker">
            <label v-for="u in matches" :key="u.id" class="picker-row">
              <input type="checkbox" :value="u.id" v-model="participants" />
              <span class="picker-name">{{ label(u) }}</span>
              <span v-if="u.display_name" class="muted small">{{ u.email }}</span>
            </label>
            <div v-if="!matches.length" class="muted small" style="padding: 0.5rem">
              Nobody matches "{{ search }}".
            </div>
          </div>
          <span class="muted small">
            {{ selected.length }} selected{{
              selected.length ? `: ${selected.map(label).join(', ')}` : ''
            }}
          </span>
        </template>
      </div>
      <button class="btn btn-primary" :disabled="creating">
        {{ creating ? 'Creating…' : 'Create chat' }}
      </button>
    </form>
  </div>

  <div class="card">
    <h2>Open an existing chat by ID</h2>
    <p class="muted small">Adds it to your list so you can jump into its messages.</p>
    <form @submit.prevent="onOpen">
      <div class="field">
        <label for="open-id">Chat ID</label>
        <input id="open-id" v-model="openId" class="mono" required />
      </div>
      <div class="row">
        <div class="field" style="flex: 1; margin: 0">
          <label for="open-label">Label (optional)</label>
          <input id="open-label" v-model="openLabel" placeholder="how it shows in your list" />
        </div>
        <div class="field" style="margin: 0">
          <label for="open-type">Type</label>
          <select id="open-type" v-model="openType">
            <option value="public">public</option>
            <option value="group">group</option>
          </select>
        </div>
      </div>
      <button class="btn" style="margin-top: 1rem">Open chat</button>
    </form>
  </div>
</template>
