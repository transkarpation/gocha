// @xmpp/client 0.14 ships no type declarations. This covers the slice the SPA
// uses: creating a client, starting/stopping it, sending stanzas, and the
// events the xmpp store listens on.

declare module '@xmpp/client' {
  /** An XMPP address. `toString()` yields the full JID. */
  export interface JID {
    local: string
    domain: string
    resource: string
    toString(): string
    bare(): JID
  }

  /** An XML element as built by `xml()` / delivered with the `stanza` event. */
  export interface Element {
    name: string
    attrs: Record<string, string>
    children: (Element | string)[]
    is(name: string, ns?: string): boolean
    getChild(name: string, ns?: string): Element | undefined
    getChildText(name: string, ns?: string): string | null
    getAttr(name: string): string | undefined
    toString(): string
  }

  export interface ClientOptions {
    /** The websocket (or BOSH/TCP) endpoint, e.g. wss://host/ws. */
    service: string
    /** XMPP domain. Derived from `service` when omitted. */
    domain?: string
    /** Resource part of the JID; the server assigns one when omitted. */
    resource?: string
    username?: string
    password?: string
  }

  /** Connection lifecycle, as emitted by the `status` event. */
  export type Status =
    | 'offline'
    | 'connecting'
    | 'connect'
    | 'opening'
    | 'open'
    | 'closing'
    | 'close'
    | 'disconnecting'
    | 'disconnect'
    | 'online'

  export interface XmppClient {
    /** Bound address; set once the client is online. */
    jid?: JID
    start(): Promise<void>
    stop(): Promise<void>
    send(element: Element): Promise<void>
    on(event: 'online', listener: (address: JID) => void): XmppClient
    on(event: 'offline', listener: () => void): XmppClient
    on(event: 'status', listener: (status: Status) => void): XmppClient
    on(event: 'error', listener: (error: Error) => void): XmppClient
    on(event: 'stanza', listener: (stanza: Element) => void): XmppClient
    removeAllListeners(): XmppClient
  }

  export function client(options: ClientOptions): XmppClient
  export function xml(
    name: string,
    attrs?: Record<string, string | undefined>,
    ...children: (Element | string)[]
  ): Element
  export function jid(local: string, domain: string, resource?: string): JID
}
