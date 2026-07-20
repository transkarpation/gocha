import { defineStore } from 'pinia'
import { computed, ref, shallowRef } from 'vue'
import { client, type Status, type XmppClient } from '@xmpp/client'

// The SPA's own XMPP connection. Signing in hands back the credentials of the
// user's mirrored chat account (xmpp_username/xmpp_password, see the auth
// store) and those are what we authenticate with here — the same thing the Go
// server does for the system account in internal/xmppclient, only over the
// browser's websocket. Without credentials (a user with no mirror) or without
// VITE_ETHORA_WS there is simply no connection; that must never break the app.

const SERVICE = import.meta.env.VITE_ETHORA_WS ?? ''

/** The XMPP domain is the websocket host, so the JID is `<username>@<host>` —
 *  this mirrors wsDomain() in internal/xmppclient/client.go. */
function serviceDomain(service: string): string {
  try {
    return new URL(service).hostname
  } catch {
    return ''
  }
}

export const useXmppStore = defineStore('xmpp', () => {
  const status = ref<Status>('offline')
  const jid = ref<string | null>(null)
  const error = ref<string | null>(null)

  // shallowRef, not ref: the client is an event emitter with cyclic internals,
  // and deep reactivity over it would be both pointless and expensive.
  const connection = shallowRef<XmppClient | null>(null)

  const isOnline = computed(() => status.value === 'online')

  /** Connect as username/password. Idempotent: calling it while a connection
   *  exists is a no-op, so the boot path and the sign-in path can both call
   *  it. Never throws — a chat connection failing must not fail the caller. */
  async function connect(username?: string, password?: string) {
    if (connection.value) return
    if (!SERVICE) {
      console.info('[xmpp] VITE_ETHORA_WS is empty — not connecting')
      return
    }
    if (!username || !password) {
      console.info('[xmpp] no chat credentials — not connecting')
      return
    }

    const domain = serviceDomain(SERVICE)
    if (!domain) {
      error.value = `Invalid VITE_ETHORA_WS: ${SERVICE}`
      return
    }

    // @xmpp/client reconnects on its own once started; `status` reports every
    // step of that lifecycle.
    const conn = client({ service: SERVICE, domain, username, password, resource: 'web' })
    connection.value = conn

    conn.on('status', (next) => {
      status.value = next
    })
    conn.on('online', (address) => {
      jid.value = address.toString()
      error.value = null
      console.info('[xmpp] online', address.toString())
    })
    conn.on('offline', () => {
      jid.value = null
    })
    // An 'error' listener is mandatory: an EventEmitter with no handler for it
    // rethrows. Log the error only — never the credentials that produced it.
    conn.on('error', (err) => {
      error.value = err.message
      console.error('[xmpp] connection error', err.message)
    })

    try {
      await conn.start()
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err)
      console.error('[xmpp] failed to connect', error.value)
    }
  }

  /** Tear the connection down (log out, or a session that went stale). */
  async function disconnect() {
    const conn = connection.value
    connection.value = null
    status.value = 'offline'
    jid.value = null
    error.value = null
    if (!conn) return
    try {
      await conn.stop()
    } catch {
      // already down — nothing to clean up
    }
    conn.removeAllListeners()
  }

  return { status, jid, error, connection, isOnline, connect, disconnect }
})
