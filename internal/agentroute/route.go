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
		item := Hop{Host: hop.Host, Port: hop.Port, User: hop.User, Password: hop.Credentials.Password, Fingerprint: hop.HostKey.PinnedSHA256}
		for _, key := range hop.Credentials.PrivateKeys {
			item.PrivateKeys = append(item.PrivateKeys, PrivateKey{PEM: append([]byte(nil), key.PEM...), Passphrase: append([]byte(nil), key.Passphrase...)})
		}
		wire.Hops = append(wire.Hops, item)
	}
	payload, err := json.Marshal(wire)
	return string(payload), err
}

func Decode(payload string) (connector.Route, error) {
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
	for _, item := range wire.Hops {
		if item.Host == "" || item.User == "" || item.Fingerprint == "" {
			return connector.Route{}, errors.New("agent route hop is incomplete")
		}
		credentials := connector.Credentials{UseAgent: false, Password: item.Password}
		for _, key := range item.PrivateKeys {
			credentials.PrivateKeys = append(credentials.PrivateKeys, connector.PrivateKey{PEM: append([]byte(nil), key.PEM...), Passphrase: append([]byte(nil), key.Passphrase...)})
		}
		route.Hops = append(route.Hops, connector.Hop{Host: item.Host, Port: item.Port, User: item.User, Credentials: credentials, HostKey: connector.HostKeyPolicy{PinnedSHA256: item.Fingerprint}})
	}
	return route, nil
}
