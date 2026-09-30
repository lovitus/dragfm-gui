package agentroute

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
)

type Route struct {
	Hops  []Hop  `json:"hops"`
	SOCKS *SOCKS `json:"socks,omitempty"`
}

type SOCKS struct {
	Address  string `json:"address"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type Hop struct {
	Host        string       `json:"host"`
	Port        int          `json:"port"`
	User        string       `json:"user"`
	Password    string       `json:"password,omitempty"`
	PrivateKeys []PrivateKey `json:"private_keys,omitempty"`
	Fingerprint string       `json:"fingerprint"`
	// Opt-in to the initiating process's own keys, never controller identity
	// discovery or a different UID's inherited agent. Absent remains false.
	AllowLocalIdentity bool `json:"allow_local_identity,omitempty"`
	// This policy is only for the actual remote UID 0, not for the controller
	// or a non-root initiator trying its own keys against another account.
	RootIdentityOnly bool `json:"root_identity_only,omitempty"`
}

type PrivateKey struct {
	PEM        []byte `json:"pem"`
	Passphrase []byte `json:"passphrase,omitempty"`
}

func Encode(route connector.Route) (string, error) {
	wire := Route{Hops: make([]Hop, 0, len(route.Hops))}
	if route.SOCKS != nil {
		wire.SOCKS = &SOCKS{Address: route.SOCKS.Address, Username: route.SOCKS.Username, Password: route.SOCKS.Password}
	}
	for _, hop := range route.Hops {
		if hop.HostKey.PinnedSHA256 == "" {
			return "", errors.New("agent route requires a pinned SSH fingerprint for every hop")
		}
		item := Hop{Host: hop.Host, Port: hop.Port, User: hop.User, Password: hop.Credentials.Password, Fingerprint: hop.HostKey.PinnedSHA256, AllowLocalIdentity: hop.Credentials.UseAgent}
		for _, key := range hop.Credentials.PrivateKeys {
			item.PrivateKeys = append(item.PrivateKeys, PrivateKey{PEM: append([]byte(nil), key.PEM...), Passphrase: append([]byte(nil), key.Passphrase...)})
		}
		wire.Hops = append(wire.Hops, item)
	}
	payload, err := json.Marshal(wire)
	return string(payload), err
}

// EncodeRootInitiator produces a wire-only final-hop identity policy. It
// deliberately cannot return a connector.Route usable by controller Dial.
// Earlier saved jump identities and host pins remain unchanged.
func EncodeRootInitiator(route connector.Route, user string) (string, error) {
	if user == "" || len(route.Hops) == 0 {
		return "", errors.New("remote root identity route is incomplete")
	}
	copyRoute := route
	copyRoute.Hops = append([]connector.Hop(nil), route.Hops...)
	final := &copyRoute.Hops[len(copyRoute.Hops)-1]
	final.User = user
	final.Credentials = connector.Credentials{UseAgent: true}
	payload, err := Encode(copyRoute)
	if err != nil {
		return "", err
	}
	var wire Route
	if err := json.Unmarshal([]byte(payload), &wire); err != nil {
		return "", err
	}
	wire.Hops[len(wire.Hops)-1].RootIdentityOnly = true
	data, err := json.Marshal(wire)
	return string(data), err
}

// An optional UID must be the executing remote process's os.Geteuid(). Without
// that explicit execution context, root-only payloads cannot become dialable.
func Decode(payload string, initiatorUID ...int) (connector.Route, error) {
	var wire Route
	if err := json.Unmarshal([]byte(payload), &wire); err != nil {
		return connector.Route{}, err
	}
	if len(wire.Hops) == 0 {
		return connector.Route{}, errors.New("agent route has no hops")
	}
	route := connector.Route{Timeout: 15 * time.Second}
	if wire.SOCKS != nil {
		route.SOCKS = &connector.SOCKS5{Address: wire.SOCKS.Address, Username: wire.SOCKS.Username, Password: wire.SOCKS.Password}
	}
	for index, item := range wire.Hops {
		if item.Host == "" || item.User == "" || item.Fingerprint == "" {
			return connector.Route{}, errors.New("agent route hop is incomplete")
		}
		if item.RootIdentityOnly && (len(initiatorUID) != 1 || initiatorUID[0] != 0 || index != len(wire.Hops)-1 || !item.AllowLocalIdentity || item.Password != "" || len(item.PrivateKeys) != 0) {
			return connector.Route{}, errors.New("remote root identity requires actual UID 0, final hop and no borrowed credentials")
		}
		// The agent service validates the effective UID before enabling its
		// socket or adding default keys. Decode alone does not read any keys.
		credentials := connector.Credentials{UseAgent: item.AllowLocalIdentity, Password: item.Password}
		for _, key := range item.PrivateKeys {
			credentials.PrivateKeys = append(credentials.PrivateKeys, connector.PrivateKey{PEM: append([]byte(nil), key.PEM...), Passphrase: append([]byte(nil), key.Passphrase...)})
		}
		route.Hops = append(route.Hops, connector.Hop{Host: item.Host, Port: item.Port, User: item.User, Credentials: credentials, HostKey: connector.HostKeyPolicy{PinnedSHA256: item.Fingerprint}})
	}
	return route, nil
}
