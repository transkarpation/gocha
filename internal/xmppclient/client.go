// Package xmppclient maintains the server's own XMPP connection: the
// system account connects to the chat platform's XMPP server over
// websocket, so the server can act in chats (e.g. send service messages)
// on its behalf.
package xmppclient

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"gosrc.io/xmpp"
)

// Run connects to the XMPP server at wsURL (a ws:// or wss:// endpoint,
// RFC 7395 websocket subprotocol) authenticating as username/password and
// keeps the connection alive, reconnecting when it drops. Every successful
// connection is logged. Run blocks until ctx is cancelled — start it in
// its own goroutine.
func Run(ctx context.Context, wsURL, username, password string) error {
	domain, err := wsDomain(wsURL)
	if err != nil {
		return err
	}
	jid := username + "@" + domain

	config := &xmpp.Config{
		TransportConfiguration: xmpp.TransportConfiguration{
			Address: wsURL,
			Domain:  domain,
		},
		Jid:        jid,
		Credential: xmpp.Password(password),
	}

	// No stanza routes yet — the connection only has to be up. Message
	// sending/receiving will register handlers on this router.
	router := xmpp.NewRouter()
	client, err := xmpp.NewClient(config, router, func(err error) {
		slog.ErrorContext(ctx, "xmpp connection error", "jid", jid, "error", err)
	})
	if err != nil {
		return fmt.Errorf("create xmpp client: %w", err)
	}

	// StreamManager reconnects with backoff; PostConnect fires on every
	// (re)connect, so each successful connection shows up in the log.
	sm := xmpp.NewStreamManager(client, func(xmpp.Sender) {
		slog.InfoContext(ctx, "connected to xmpp server", "jid", jid, "url", wsURL)
	})
	go func() {
		<-ctx.Done()
		sm.Stop()
	}()
	return sm.Run()
}

// wsDomain derives the XMPP domain from the websocket endpoint host.
func wsDomain(wsURL string) (string, error) {
	u, err := url.Parse(wsURL)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("invalid xmpp websocket url %q", wsURL)
	}
	return u.Hostname(), nil
}
