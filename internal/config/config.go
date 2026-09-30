package config

import "time"

const CurrentVersion = 2

type Document struct {
	Version    int               `json:"version"`
	Hosts      []Host            `json:"hosts,omitempty"`
	Keys       []PrivateKey      `json:"keys,omitempty"`
	SOCKS      []SOCKSProxy      `json:"socks,omitempty"`
	Jumps      []JumpRoute       `json:"jumps,omitempty"`
	Relays     []RelaySuccess    `json:"relay_success,omitempty"`
	History    []HistoryEntry    `json:"history,omitempty"`
	Workspaces []WorkspaceRecord `json:"workspaces,omitempty"`
	UI         UIState           `json:"ui"`
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
	RootKeyIDs      []string `json:"root_key_ids,omitempty"` // Explicit vault keys for the final high-privilege account.
	SudoPassword    string   `json:"sudo_password,omitempty"`
	KeyIDs          []string `json:"key_ids,omitempty"`
	HostFingerprint string   `json:"host_fingerprint,omitempty"`
	Shell           string   `json:"shell,omitempty"`
	LastDirectory   string   `json:"last_directory,omitempty"`
	DefaultSOCKSID  string   `json:"default_socks_id,omitempty"`
	DefaultJumpID   string   `json:"default_jump_id,omitempty"`
	Disabled        bool     `json:"disabled,omitempty"`
	NoRelay         bool     `json:"no_relay,omitempty"` // Keep usable as an endpoint, never an automatic relay.
	RouteSpec       string   `json:"route_spec,omitempty"`
	HopFingerprints []string `json:"hop_fingerprints,omitempty"`
	HopPasswords    []string `json:"hop_passwords,omitempty"`
	// V1 overlays did not identify the original owner of a composed-route hop.
	// Retain them for manual recovery only; never use them for authentication.
	UnverifiedPasswords []string `json:"unverified_passwords,omitempty"`
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
	State       string    `json:"state,omitempty"`
	Output      string    `json:"output,omitempty"`
}

// Recovery journal lives only inside the encrypted vault. This is ownership
// metadata, never a resumable Pending job or a second copy of credentials.
// An empty DirectoryID is a write-ahead intent: mkdir/marker creation may not
// have completed. Deletion still requires the exact private ownership marker.
type WorkspaceRecord struct {
	HostID       string          `json:"host_id"`
	Fingerprint  string          `json:"fingerprint"`
	MachineID    string          `json:"machine_id,omitempty"`
	Elevated     bool            `json:"elevated,omitempty"`
	Path         string          `json:"path"`
	ParentID     string          `json:"parent_id"`
	DirectoryID  string          `json:"directory_id,omitempty"`
	OwnerUID     uint32          `json:"owner_uid"`
	MarkerSHA256 string          `json:"marker_sha256"`
	CreatedAt    time.Time       `json:"created_at"`
	Partials     []PartialRecord `json:"partials,omitempty"`
}

// Partial ownership is tied to the installation's writer lease. FileID is
// empty until an actual inode has been observed; such an intent alone never
// authorizes recovery to remove an existing file.
type PartialRecord struct {
	Path     string `json:"path"`
	ParentID string `json:"parent_id"`
	FileID   string `json:"file_id,omitempty"`
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

// Clone detaches slices before a document is used outside its owner's lock.
func (d Document) Clone() Document {
	d.Hosts = append([]Host(nil), d.Hosts...)
	for i := range d.Hosts {
		d.Hosts[i].KeyIDs = append([]string(nil), d.Hosts[i].KeyIDs...)
		d.Hosts[i].RootKeyIDs = append([]string(nil), d.Hosts[i].RootKeyIDs...)
		d.Hosts[i].HopPasswords = append([]string(nil), d.Hosts[i].HopPasswords...)
		d.Hosts[i].UnverifiedPasswords = append([]string(nil), d.Hosts[i].UnverifiedPasswords...)
		d.Hosts[i].HopFingerprints = append([]string(nil), d.Hosts[i].HopFingerprints...)
	}
	d.Keys = append([]PrivateKey(nil), d.Keys...)
	d.SOCKS = append([]SOCKSProxy(nil), d.SOCKS...)
	d.Jumps = append([]JumpRoute(nil), d.Jumps...)
	for i := range d.Jumps {
		d.Jumps[i].HostIDs = append([]string(nil), d.Jumps[i].HostIDs...)
	}
	d.Relays = append([]RelaySuccess(nil), d.Relays...)
	d.History = append([]HistoryEntry(nil), d.History...)
	d.Workspaces = append([]WorkspaceRecord(nil), d.Workspaces...)
	for i := range d.Workspaces {
		d.Workspaces[i].Partials = append([]PartialRecord(nil), d.Workspaces[i].Partials...)
	}
	return d
}
