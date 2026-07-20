import client from '@/api/client'
import type { DirectoryUser, Role, User } from '@/types'

export interface ListUsersParams {
  limit?: number
  offset?: number
}

/** Admin-only: the full listing, with roles and timestamps. */
export function listUsers(params: ListUsersParams = {}) {
  return client.get<{ users: User[] }>('/users', { params })
}

/** Open to every signed-in user: who you can add to a chat. Excludes you and
 *  the system account server-side. `limit` maxes out at 100. */
export function userDirectory(params: ListUsersParams = {}) {
  return client.get<{ users: DirectoryUser[] }>('/users/directory', { params })
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
