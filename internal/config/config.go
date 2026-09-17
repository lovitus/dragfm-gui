package config

import "time"

const CurrentVersion = 1

type Document struct {
	Version int            `json:"version"`
	Hosts   []Host         `json:"hosts,omitempty"`
	Keys    []PrivateKey   `json:"keys,omitempty"`
	SOCKS   []SOCKSProxy   `json:"socks,omitempty"`
	Jumps   []JumpRoute    `json:"jumps,omitempty"`
	Relays  []RelaySuccess `json:"relay_success,omitempty"`
	History []HistoryEntry `json:"history,omitempty"`
	UI      UIState        `json:"ui"`
}

type Host struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Address         string   `json:"address"`
	Port            int      `json:"port"`
	User            string   `json:"user"`
	Password        string   `json:"password,omitempty"`
	RootUser        string   `json:"root_user,omitempty"`
	RootPassword    string   `json:"root_password,omitempty"`
	SudoPassword    string   `json:"sudo_password,omitempty"`
	KeyIDs          []string `json:"key_ids,omitempty"`
	HostFingerprint string   `json:"host_fingerprint,omitempty"`
	Shell           string   `json:"shell,omitempty"`
	LastDirectory   string   `json:"last_directory,omitempty"`
	DefaultSOCKSID  string   `json:"default_socks_id,omitempty"`
	DefaultJumpID   string   `json:"default_jump_id,omitempty"`
	Disabled        bool     `json:"disabled,omitempty"`
	RouteSpec       string   `json:"route_spec,omitempty"`
	HopFingerprints []string `json:"hop_fingerprints,omitempty"`
	HopPasswords    []string `json:"hop_passwords,omitempty"`
}

type PrivateKey struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PEM         string `json:"pem"`
	Passphrase  string `json:"passphrase,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type SOCKSProxy struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Address     string    `json:"address"`
	Username    string    `json:"username,omitempty"`
	Password    string    `json:"password,omitempty"`
	Disabled    bool      `json:"disabled,omitempty"`
	LastRTT     int64     `json:"last_rtt_ms,omitempty"`
	LastSuccess time.Time `json:"last_success,omitempty"`
	Spec        string    `json:"spec,omitempty"`
}

type JumpRoute struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	HostIDs  []string `json:"host_ids"`
	Disabled bool     `json:"disabled,omitempty"`
	LastRTT  int64    `json:"last_rtt_ms,omitempty"`
}

// RelaySuccess remembers only a route that actually completed an SSH
// handshake for this endpoint pair. It intentionally stores IDs rather than
// addresses or credentials, limiting both disclosure and stale duplication.
type RelaySuccess struct {
	EndpointAID string    `json:"endpoint_a_id"`
	EndpointBID string    `json:"endpoint_b_id"`
	RelayHostID string    `json:"relay_host_id"`
	LastSuccess time.Time `json:"last_success"`
}

type HistoryEntry struct {
	ID          string    `json:"id"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	Operation   string    `json:"operation"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	Method      string    `json:"method"`
	Success     bool      `json:"success"`
	Message     string    `json:"message,omitempty"`
}

type UIState struct {
	LeftEndpoint  string  `json:"left_endpoint,omitempty"`
	RightEndpoint string  `json:"right_endpoint,omitempty"`
	LeftPath      string  `json:"left_path,omitempty"`
	RightPath     string  `json:"right_path,omitempty"`
	Theme         string  `json:"theme,omitempty"`
	Width         float32 `json:"width,omitempty"`
	Height        float32 `json:"height,omitempty"`
}

func NewDocument() Document {
	return Document{Version: CurrentVersion, UI: UIState{Theme: "system", Width: 1440, Height: 860}}
}

func (d *Document) HostByID(id string) *Host {
	for i := range d.Hosts {
		if d.Hosts[i].ID == id {
			return &d.Hosts[i]
		}
	}
	return nil
}

func (d *Document) HostByName(name string) (Host, bool) {
	for i := range d.Hosts {
		if d.Hosts[i].Name == name {
			return d.Hosts[i], true
		}
	}
	return Host{}, false
}

func (d *Document) KeyByID(id string) *PrivateKey {
	for i := range d.Keys {
		if d.Keys[i].ID == id {
			return &d.Keys[i]
		}
	}
	return nil
}

func (d *Document) SOCKSByID(id string) *SOCKSProxy {
	for i := range d.SOCKS {
		if d.SOCKS[i].ID == id {
			return &d.SOCKS[i]
		}
	}
	return nil
}
