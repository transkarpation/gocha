import client from '@/api/client'
import type { AuthResponse, Me } from '@/types'

export function register(email: string, password: string, displayName: string) {
  return client.post<AuthResponse>('/register', {
    email,
    password,
    display_name: displayName,
  })
}

export function login(email: string, password: string) {
  return client.post<AuthResponse>('/login', { email, password })
}

export function me() {
  return client.get<Me>('/me')
}
