package xmppclient

import "testing"

func TestWSDomain(t *testing.T) {
	tests := []struct {
		url     string
		want    string
		wantErr bool
	}{
		{url: "wss://xmpp.chat.ethora.com/ws", want: "xmpp.chat.ethora.com"},
		{url: "ws://localhost:5443/ws", want: "localhost"},
		{url: "wss://host.example:5443", want: "host.example"},
		{url: "not a url", wantErr: true},
		{url: "", wantErr: true},
	}
	for _, tt := range tests {
		got, err := wsDomain(tt.url)
		if tt.wantErr {
			if err == nil {
				t.Errorf("wsDomain(%q) = %q, want error", tt.url, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("wsDomain(%q): %v", tt.url, err)
			continue
		}
		if got != tt.want {
			t.Errorf("wsDomain(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}
