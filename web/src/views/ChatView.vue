<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'

import { useAuthStore } from '@/stores/auth'
import { useChatsStore } from '@/stores/chats'
import { addParticipants, listMessages, sendMessage } from '@/services/chats.service'
import { userDirectory } from '@/services/users.service'
import { errorMessage } from '@/api/client'
import type { DirectoryUser, Message } from '@/types'

const route = useRoute()
const auth = useAuthStore()
const chatsStore = useChatsStore()

const chatId = computed(() => String(route.params.id))
const known = computed(() => chatsStore.get(chatId.value))

const PAGE = 50

const messages = ref<Message[]>([])
const error = ref('')
const loading = ref(false)
const hasMore = ref(false)
const text = ref('')
const sending = ref(false)
const autoRefresh = ref(true)
const listEl = ref<HTMLElement | null>(null)

let timer: ReturnType<typeof setInterval> | null = null

// The API returns messages newest-first; we render oldest-first (chat order),
// so reverse before storing.
async function loadLatest(scroll = true) {
  error.value = ''
  loading.value = true
  try {
    const { data } = await listMessages(chatId.value, { limit: PAGE, offset: 0 })
    messages.value = [...data.messages].reverse()
    hasMore.value = data.messages.length === PAGE
    if (scroll) await scrollToBottom()
  } catch (e) {
    error.value = errorMessage(e, 'Could not load messages')
    stopAuto()
  } finally {
    loading.value = false
  }
}

async function loadOlder() {
  loading.value = true
  try {
    const { data } = await listMessages(chatId.value, { limit: PAGE, offset: messages.value.length })
    // Older page is also newest-first; reversed it sits before what we have.
    messages.value = [...[...data.messages].reverse(), ...messages.value]
    hasMore.value = data.messages.length === PAGE
  } catch (e) {
    error.value = errorMessage(e, 'Could not load older messages')
  } finally {
    loading.value = false
  }
}

async function onSend() {
  const body = text.value.trim()
  if (!body) return
  sending.value = true
  error.value = ''
  try {
    const { data } = await sendMessage(chatId.value, body)
    messages.value.push(data)
    text.value = ''
    await scrollToBottom()
  } catch (e) {
    error.value = errorMessage(e, 'Could not send message')
  } finally {
    sending.value = false
  }
}

async function scrollToBottom() {
  await nextTick()
  if (listEl.value) listEl.value.scrollTop = listEl.value.scrollHeight
}

function startAuto() {
  stopAuto()
  timer = setInterval(() => {
    // Only pull if the user is near the bottom, so a refresh doesn't yank
    // them away from older messages they're reading.
    const el = listEl.value
    const nearBottom = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 120
    loadLatest(nearBottom)
  }, 5000)
}

function stopAuto() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

watch(autoRefresh, (on) => (on ? startAuto() : stopAuto()))
watch(chatId, () => loadLatest())

function isMine(m: Message) {
  return m.author_id === auth.user?.id
}

function authorLabel(m: Message) {
  return isMine(m) ? 'You' : m.author_id.slice(-6)
}

function formatTime(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString()
}

// --- Adding participants ---
// There is no GET /chats/{id}, so the SPA cannot tell whether you are this
// chat's creator, nor who is already in it. The panel is therefore offered
// to everyone and the server has the final say: a 403 lands in addError.
// Re-adding an existing participant is a no-op server-side, so an entry
// already in the chat does no harm.
const showAdd = ref(false)
const directory = ref<DirectoryUser[]>([])
const toAdd = ref<string[]>([])
const adding = ref(false)
const addError = ref('')
const addNotice = ref('')
const roster = ref<string[] | null>(null)

function label(u: DirectoryUser) {
  return u.display_name || u.email
}

async function togglePanel() {
  showAdd.value = !showAdd.value
  addError.value = ''
  addNotice.value = ''
  if (!showAdd.value || directory.value.length) return
  try {
    const { data } = await userDirectory({ limit: 100 })
    directory.value = data.users
  } catch (e) {
    addError.value = errorMessage(e, 'Could not load the user list')
  }
}

async function onAdd() {
  if (!toAdd.value.length) return
  adding.value = true
  addError.value = ''
  addNotice.value = ''
  try {
    const { data } = await addParticipants(chatId.value, toAdd.value)
    roster.value = data.participants
    addNotice.value = `Added. The chat now has ${data.participants.length} participants.`
    toAdd.value = []
  } catch (e) {
    addError.value = errorMessage(e, 'Could not add participants')
  } finally {
    adding.value = false
  }
}

onMounted(() => {
  loadLatest()
  if (autoRefresh.value) startAuto()
})
onBeforeUnmount(stopAuto)
</script>

<template>
  <div class="row-between" style="margin-bottom: 1rem">
    <div>
      <h1 style="margin: 0">{{ known?.name ?? 'Chat' }}</h1>
      <span class="muted small mono">{{ chatId }}</span>
      <span v-if="known" class="badge" :class="known.type" style="margin-left: 0.5rem">
        {{ known.type }}
      </span>
    </div>
    <div class="row" style="gap: 0.4rem">
      <button class="btn btn-sm" @click="togglePanel">
        {{ showAdd ? 'Close' : 'Add participants' }}
      </button>
      <RouterLink class="btn btn-sm" :to="{ name: 'chats' }">← All chats</RouterLink>
    </div>
  </div>

  <div v-if="error" class="alert alert-error">{{ error }}</div>

  <div v-if="showAdd" class="card">
    <h2 style="margin-top: 0">Add participants</h2>
    <p class="muted small">
      Only the chat's creator (or an admin) may change who is in it. Someone
      already in the chat is skipped.
    </p>

    <div v-if="addError" class="alert alert-error">{{ addError }}</div>
    <div v-if="addNotice" class="alert alert-success">{{ addNotice }}</div>

    <div v-if="!directory.length && !addError" class="muted small">Loading users…</div>
    <template v-else-if="directory.length">
      <div class="picker">
        <label v-for="u in directory" :key="u.id" class="picker-row">
          <input type="checkbox" :value="u.id" v-model="toAdd" />
          <span class="picker-name">{{ label(u) }}</span>
          <span v-if="u.display_name" class="muted small">{{ u.email }}</span>
        </label>
      </div>
      <button
        class="btn btn-primary"
        style="margin-top: 1rem"
        :disabled="adding || !toAdd.length"
        @click="onAdd"
      >
        {{ adding ? 'Adding…' : `Add ${toAdd.length || ''}`.trim() }}
      </button>
    </template>

    <p v-if="roster" class="muted small" style="margin-top: 1rem">
      Participant ids: <span class="mono">{{ roster.join(', ') }}</span>
    </p>
  </div>

  <div class="card" style="padding: 0; overflow: hidden">
    <div class="row-between" style="padding: 0.75rem 1rem; border-bottom: 1px solid var(--border)">
      <label class="row small muted" style="gap: 0.4rem; cursor: pointer">
        <input v-model="autoRefresh" type="checkbox" style="width: auto" />
        Auto-refresh
      </label>
      <button class="btn btn-sm" :disabled="loading" @click="loadLatest()">Refresh</button>
    </div>

    <div ref="listEl" class="msg-list">
      <div v-if="hasMore" style="text-align: center; margin-bottom: 0.75rem">
        <button class="btn btn-sm" :disabled="loading" @click="loadOlder">Load older</button>
      </div>

      <div v-if="!messages.length && !loading" class="empty">
        No messages yet. Say something below.
      </div>

      <div
        v-for="m in messages"
        :key="m.id"
        class="msg"
        :class="{ mine: isMine(m) }"
      >
        <div class="msg-bubble">
          <div class="msg-meta">
            <span class="msg-author">{{ authorLabel(m) }}</span>
            <span class="msg-time">{{ formatTime(m.created_at) }}</span>
          </div>
          <div class="msg-text">{{ m.text }}</div>
        </div>
      </div>
    </div>

    <form class="composer" @submit.prevent="onSend">
      <textarea
        v-model="text"
        rows="1"
        maxlength="2000"
        placeholder="Type a message…"
        @keydown.enter.exact.prevent="onSend"
      />
      <button class="btn btn-primary" :disabled="sending || !text.trim()">
        {{ sending ? 'Sending…' : 'Send' }}
      </button>
    </form>
  </div>
</template>

<style scoped>
.msg-list {
  height: 55vh;
  min-height: 300px;
  overflow-y: auto;
  padding: 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.msg {
  display: flex;
}

.msg.mine {
  justify-content: flex-end;
}

.msg-bubble {
  max-width: 78%;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 0.5rem 0.75rem;
}

.msg.mine .msg-bubble {
  background: rgba(91, 140, 255, 0.16);
  border-color: rgba(91, 140, 255, 0.35);
}

.msg-meta {
  display: flex;
  gap: 0.6rem;
  align-items: baseline;
  margin-bottom: 0.15rem;
}

.msg-author {
  font-weight: 600;
  font-size: 0.8rem;
}

.msg-time {
  font-size: 0.72rem;
  color: var(--text-dim);
}

.msg-text {
  white-space: pre-wrap;
  word-break: break-word;
}

.composer {
  display: flex;
  gap: 0.6rem;
  padding: 0.75rem 1rem;
  border-top: 1px solid var(--border);
  align-items: flex-end;
}

.composer textarea {
  flex: 1;
}
</style>
