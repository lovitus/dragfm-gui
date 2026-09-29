package agentroute

import (
	"encoding/json"
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

// For the red run, withdraw the policy guards in the candidate while retaining
// the new wire API. A missing function/signature is not behavioral red evidence.
func TestRootIdentityIsBoundToActualRemoteUID(t *testing.T) {
	ordinary := connector.Route{Hops: []connector.Hop{
		{Host: "relay.invalid", Port: 22, User: "jump", Credentials: connector.Credentials{Password: "jump-bound"}, HostKey: connector.HostKeyPolicy{PinnedSHA256: "jump-pin"}},
		{Host: "peer.invalid", Port: 22, User: "regular", Credentials: connector.Credentials{UseAgent: true, Password: "ordinary-bound", PrivateKeys: []connector.PrivateKey{{PEM: []byte("ordinary-key")}}}, HostKey: connector.HostKeyPolicy{PinnedSHA256: "peer-pin"}},
	}}
	payload, err := EncodeRootInitiator(ordinary, "root")
	if err != nil {
		t.Fatal(err)
	}
	if ordinary.Hops[1].User != "regular" || ordinary.Hops[1].Credentials.Password != "ordinary-bound" || len(ordinary.Hops[1].Credentials.PrivateKeys) != 1 {
		t.Fatal("root wire conversion mutated the controller route")
	}
	for _, candidate := range []struct {
		name    string
		uid     []int
		alter   func(*Route)
		allowed bool
	}{
		{name: "controller-without-execution-context"},
		{name: "ordinary-initiator", uid: []int{1000}},
		{name: "root-initiator", uid: []int{0}, allowed: true},
		{name: "password-mixed-into-implicit-identity", uid: []int{0}, alter: func(wire *Route) { wire.Hops[1].Password = "borrowed" }},
		{name: "key-mixed-into-implicit-identity", uid: []int{0}, alter: func(wire *Route) { wire.Hops[1].PrivateKeys = []PrivateKey{{PEM: []byte("borrowed")}} }},
		{name: "discovery-not-authorized", uid: []int{0}, alter: func(wire *Route) { wire.Hops[1].AllowLocalIdentity = false }},
		{name: "policy-on-a-jump", uid: []int{0}, alter: func(wire *Route) {
			wire.Hops[0].RootIdentityOnly, wire.Hops[0].AllowLocalIdentity = true, true
			wire.Hops[0].Password = ""
		}},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			var wire Route
			if err := json.Unmarshal([]byte(payload), &wire); err != nil {
				t.Fatal(err)
			}
			if candidate.alter != nil {
				candidate.alter(&wire)
			}
			data, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode(string(data), candidate.uid...)
			if !candidate.allowed {
				if err == nil {
					t.Fatal("root-only policy became dialable outside its permitted identity")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			final := decoded.Hops[1]
			if final.User != "root" || !final.Credentials.UseAgent || final.Credentials.Password != "" || len(final.Credentials.PrivateKeys) != 0 || final.HostKey.PinnedSHA256 != "peer-pin" {
				t.Fatal("root route borrowed an ordinary credential or lost its pin")
			}
			if decoded.Hops[0].User != "jump" || decoded.Hops[0].Credentials.Password != "jump-bound" || decoded.Hops[0].HostKey.PinnedSHA256 != "jump-pin" {
				t.Fatal("earlier hop identity was changed")
			}
		})
	}
}

func TestRouteRejectsUnpinnedHop(t *testing.T) {
	if _, err := Encode(connector.Route{Hops: []connector.Hop{{Host: "host", User: "u"}}}); err == nil {
		t.Fatal("unpinned route accepted")
	}
}

func TestPerHopLocalIdentityPolicyRoundTrip(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		route := connector.Route{Hops: []connector.Hop{
			{Host: "relay.invalid", User: "relay", Credentials: connector.Credentials{UseAgent: allowed}, HostKey: connector.HostKeyPolicy{PinnedSHA256: "relay-pin"}},
			{Host: "target.invalid", User: "root", Credentials: connector.Credentials{UseAgent: false, PrivateKeys: []connector.PrivateKey{{PEM: []byte("explicit-only")}}}, HostKey: connector.HostKeyPolicy{PinnedSHA256: "target-pin"}},
		}}
		wire, err := Encode(route)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(wire)
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Hops[0].Credentials.UseAgent != allowed || decoded.Hops[1].Credentials.UseAgent {
			t.Fatal("local identity consent was dropped or spread to an explicitly restricted hop")
		}
		if string(decoded.Hops[1].Credentials.PrivateKeys[0].PEM) != "explicit-only" {
			t.Fatal("explicit key was lost")
		}
	}
	legacy, err := Decode(`{"hops":[{"host":"target.invalid","user":"root","fingerprint":"pin"}]}`)
	if err != nil || legacy.Hops[0].Credentials.UseAgent {
		t.Fatal("absent policy must not enable implicit credentials")
	}
}
