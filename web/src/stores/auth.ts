import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { getToken, setToken } from '@/api/client'
import * as authService from '@/services/auth.service'
import { useXmppStore } from '@/stores/xmpp'
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

  // Open the XMPP connection with the stored chat credentials. Called after
  // signing in and once on boot (the creds outlive a reload in localStorage);
  // it is idempotent and silent when there are no credentials.
  function connectXmpp() {
    return useXmppStore().connect(xmpp.value?.username, xmpp.value?.password)
  }

  // Register / log in, store the token and XMPP creds, then resolve the role
  // via /me (the sign-in payload deliberately omits it). Errors bubble up so
  // the component can render them; the caller catches. The chat connection is
  // deliberately not awaited: it must not delay or fail signing in.
  async function register(email: string, password: string, displayName: string) {
    const { data } = await authService.register(email, password, displayName)
    applyToken(data.access_token)
    applyXmpp(data.xmpp_username, data.xmpp_password)
    void connectXmpp()
    await fetchMe()
  }

  async function login(email: string, password: string) {
    const { data } = await authService.login(email, password)
    applyToken(data.access_token)
    applyXmpp(data.xmpp_username, data.xmpp_password)
    void connectXmpp()
    await fetchMe()
  }

  async function fetchMe() {
    const { data } = await authService.me()
    user.value = data
    return data
  }

  function logout() {
    void useXmppStore().disconnect()
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
    connectXmpp,
    logout,
  }
})
