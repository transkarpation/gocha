import client from '@/api/client'
import type { Chat, ChatType, Message } from '@/types'

export interface CreateChatPayload {
  name: string
  type: ChatType
  participants?: string[]
}

export function createChat(payload: CreateChatPayload) {
  return client.post<Chat>('/chats', payload)
}

export function deleteChat(id: string) {
  return client.delete(`/chats/${id}`)
}

export interface ListMessagesParams {
  limit?: number
  offset?: number
}

export function listMessages(chatId: string, params: ListMessagesParams = {}) {
  return client.get<{ messages: Message[] }>(`/chats/${chatId}/messages`, { params })
}

export function sendMessage(chatId: string, text: string) {
  return client.post<Message>(`/chats/${chatId}/messages`, { text })
}
