import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import type { ChatType } from '@/types'

// The API has no "list my chats" endpoint — you can create, delete, and read
// messages, but there is no way to enumerate chats server-side. So the SPA
// keeps its own registry of chats you have created or opened by ID, persisted
// in localStorage. This is a client-side convenience, not a source of truth:
// a chat can exist on the server without appearing here, and clearing browser
// storage forgets the list (the chats themselves are untouched).

export interface KnownChat {
  id: string
  name: string
  type: ChatType
}

const STORAGE_KEY = 'gocha_chats'

function load(): KnownChat[] {
  const raw = localStorage.getItem(STORAGE_KEY)
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? (parsed as KnownChat[]) : []
  } catch {
    return []
  }
}

export const useChatsStore = defineStore('chats', () => {
  const chats = ref<KnownChat[]>(load())

  const sorted = computed(() =>
    [...chats.value].sort((a, b) => a.name.localeCompare(b.name)),
  )

  function persist() {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(chats.value))
  }

  function get(id: string): KnownChat | undefined {
    return chats.value.find((c) => c.id === id)
  }

  // Add or update a known chat (upsert by id), then persist.
  function remember(chat: KnownChat) {
    const existing = chats.value.find((c) => c.id === chat.id)
    if (existing) {
      existing.name = chat.name
      existing.type = chat.type
    } else {
      chats.value.push(chat)
    }
    persist()
  }

  function forget(id: string) {
    chats.value = chats.value.filter((c) => c.id !== id)
    persist()
  }

  return { chats, sorted, get, remember, forget }
})
