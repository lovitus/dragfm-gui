package agentroute

import (
	"testing"

	"github.com/flyssh/flyssh/pkg/connector"
)

func TestRouteRoundTripKeepsCredentialsAndPins(t *testing.T) {
	route := connector.Route{SOCKS: &connector.SOCKS5{Address: "127.0.0.1:1080", Username: "su", Password: "sp"}, Hops: []connector.Hop{{Host: "10.0.0.1", Port: 22, User: "u", Credentials: connector.Credentials{Password: "p", PrivateKeys: []connector.PrivateKey{{PEM: []byte("pem"), Passphrase: []byte("phrase")}}}, HostKey: connector.HostKeyPolicy{PinnedSHA256: "SHA256:pin"}}}}
	payload, err := Encode(route)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(payload)
	if err != nil {
		t.Fatal(err)
	}
	hop := decoded.Hops[0]
	if hop.Credentials.Password != "p" || string(hop.Credentials.PrivateKeys[0].PEM) != "pem" || hop.HostKey.PinnedSHA256 != "SHA256:pin" || decoded.SOCKS == nil || decoded.SOCKS.Password != "sp" {
		t.Fatalf("lost route data: %#v", hop)
	}
}

func TestRouteRejectsUnpinnedHop(t *testing.T) {
	if _, err := Encode(connector.Route{Hops: []connector.Hop{{Host: "host", User: "u"}}}); err == nil {
		t.Fatal("unpinned route accepted")
	}
}
