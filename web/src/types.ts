// Shapes returned by the gocha API. See internal/users/handler.go and
// internal/chats/handler.go for the authoritative definitions.

export type Role = 'admin' | 'user'

/** POST /register and POST /login response. Note: no `role` here — the sign-in
 *  payload only proves who you are; call GET /me to learn the role. XMPP
 *  credentials are omitted when the user has no mirrored chat account. */
export interface AuthResponse {
  id: string
  email: string
  /** Empty for accounts registered without one (the CLI, older accounts). */
  display_name: string
  access_token: string
  expires_at: string
  xmpp_username?: string
  xmpp_password?: string
}

/** GET /me response. */
export interface Me {
  id: string
  email: string
  display_name: string
  role: Role
}

/** A user as returned by GET /users, PATCH /users/{id}, restore. */
export interface User {
  id: string
  email: string
  display_name: string
  role: Role
  created_at: string
}

export type ChatType = 'public' | 'group'

/** POST /chats response. */
export interface Chat {
  id: string
  name: string
  type: ChatType
  participants: string[]
  created_by: string
  created_at: string
}

/** A message as returned by the messages endpoints. */
export interface Message {
  id: string
  chat_id: string
  author_id: string
  text: string
  created_at: string
}
