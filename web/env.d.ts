/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Base URL of the gocha API. Empty = same-origin. */
  readonly VITE_API_URL: string
  /** XMPP websocket endpoint (RFC 7395), e.g. wss://xmpp.chat.ethora.com/ws.
   *  Empty disables the SPA's chat connection. */
  readonly VITE_ETHORA_WS: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
