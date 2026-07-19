import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { getToken, setToken } from '@/api/client'
import * as authService from '@/services/auth.service'
import type { Me } from '@/types'

const XMPP_KEY = 'gocha_xmpp'

interface XmppCredentials {
  username: string
  password: string
}

function loadXmpp(): XmppCredentials | null {
  const raw = localStorage.getItem(XMPP_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as XmppCredentials
  } catch {
    return null
  }
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(getToken())
  const user = ref<Me | null>(null)
  const xmpp = ref<XmppCredentials | null>(loadXmpp())

  const isAuthenticated = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  function applyToken(newToken: string | null) {
    token.value = newToken
    setToken(newToken)
  }

  function applyXmpp(username?: string, password?: string) {
    if (username && password) {
      xmpp.value = { username, password }
      localStorage.setItem(XMPP_KEY, JSON.stringify(xmpp.value))
    } else {
      xmpp.value = null
      localStorage.removeItem(XMPP_KEY)
    }
  }

  // Register / log in, store the token and XMPP creds, then resolve the role
  // via /me (the sign-in payload deliberately omits it). Errors bubble up so
  // the component can render them; the caller catches.
  async function register(email: string, password: string, displayName: string) {
    const { data } = await authService.register(email, password, displayName)
    applyToken(data.access_token)
    applyXmpp(data.xmpp_username, data.xmpp_password)
    await fetchMe()
  }

  async function login(email: string, password: string) {
    const { data } = await authService.login(email, password)
    applyToken(data.access_token)
    applyXmpp(data.xmpp_username, data.xmpp_password)
    await fetchMe()
  }

  async function fetchMe() {
    const { data } = await authService.me()
    user.value = data
    return data
  }

  function logout() {
    applyToken(null)
    applyXmpp()
    user.value = null
  }

  return {
    token,
    user,
    xmpp,
    isAuthenticated,
    isAdmin,
    register,
    login,
    fetchMe,
    logout,
  }
})
