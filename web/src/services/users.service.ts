import client from '@/api/client'
import type { Role, User } from '@/types'

export interface ListUsersParams {
  limit?: number
  offset?: number
}

export function listUsers(params: ListUsersParams = {}) {
  return client.get<{ users: User[] }>('/users', { params })
}

export interface UpdateUserPayload {
  email?: string
  password?: string
  role?: Role
  /** An explicit empty string clears the name; omit the key to leave it. */
  display_name?: string
}

export function updateUser(id: string, payload: UpdateUserPayload) {
  return client.patch<User>(`/users/${id}`, payload)
}

export function deleteUser(id: string) {
  return client.delete(`/users/${id}`)
}

export function restoreUser(id: string) {
  return client.post<User>(`/users/${id}/restore`)
}
