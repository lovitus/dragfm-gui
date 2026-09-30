package webgui

import "time"

type PaneID string

const (
	LeftPane  PaneID = "left"
	RightPane PaneID = "right"
)

func validPane(value PaneID) bool { return value == LeftPane || value == RightPane }

type VaultStatusModel struct {
	Exists   bool   `json:"exists"`
	Hint     string `json:"hint"`
	Unlocked bool   `json:"unlocked"`
}

type BootstrapModel struct {
	Unlocked      bool                `json:"unlocked"`
	Hosts         []string            `json:"hosts"`
	LeftEndpoint  string              `json:"leftEndpoint"`
	RightEndpoint string              `json:"rightEndpoint"`
	LeftPath      string              `json:"leftPath"`
	RightPath     string              `json:"rightPath"`
	Theme         string              `json:"theme"`
	History       []HistoryEntryModel `json:"history"`
}

type HistoryEntryModel struct {
	ID         string `json:"id"`
	Operation  string `json:"operation"`
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Method     string `json:"method,omitempty"`
	State      string `json:"state,omitempty"`
	Output     string `json:"output,omitempty"`
	FinishedAt string `json:"finishedAt"`
}

type FileEntryModel struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	Size      int64  `json:"size"`
	Modified  string `json:"modified"`
	Directory bool   `json:"directory"`
	Symlink   bool   `json:"symlink"`
}

type DirectoryListing struct {
	Warning      string           `json:"warning,omitempty"`
	ConnectionID uint64           `json:"connectionID"`
	Pane         PaneID           `json:"pane"`
	Endpoint     string           `json:"endpoint"`
	Path         string           `json:"path"`
	Entries      []FileEntryModel `json:"entries"`
}

type DropPreview struct {
	SourcePane           PaneID `json:"sourcePane"`
	DestinationPane      PaneID `json:"destinationPane"`
	SourcePath           string `json:"sourcePath"`
	DestinationDirectory string `json:"destinationDirectory"`
	TargetPath           string `json:"targetPath"`
	Name                 string `json:"name"`
	Conflict             bool   `json:"conflict"`
}

type TransferRequest struct {
	DropPreview
	Move      bool `json:"move"`
	Overwrite bool `json:"overwrite"`
}

type JobUpdateModel struct {
	Revision      uint64  `json:"revision"`
	ID            string  `json:"id"`
	State         string  `json:"state"`
	Description   string  `json:"description"`
	Message       string  `json:"message"`
	Output        string  `json:"output,omitempty"`
	Progress      float64 `json:"progress"`
	ProgressKnown bool    `json:"progressKnown"`
	Indeterminate bool    `json:"indeterminate"`
	Stage         string  `json:"stage,omitempty"`
	Method        string  `json:"method,omitempty"`
	BytesDone     int64   `json:"bytesDone,omitempty"`
	BytesTotal    int64   `json:"bytesTotal,omitempty"`
	FilesDone     int     `json:"filesDone,omitempty"`
	FilesTotal    int     `json:"filesTotal,omitempty"`
	StartedAt     string  `json:"startedAt,omitempty"`
	FinishedAt    string  `json:"finishedAt,omitempty"`
}

type ConfigTexts struct {
	Markdown string `json:"markdown"`
	Revision string `json:"revision"`
}

type ConnectionRow struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Disabled     bool   `json:"disabled"`
	RelayAllowed bool   `json:"relayAllowed"`
	RelayReady   bool   `json:"relayReady"`
	LastRTT      int64  `json:"lastRTT"`
	LastSuccess  string `json:"lastSuccess"`
}

type RelayCacheModel struct {
	EndpointAID string `json:"endpointAID"`
	EndpointBID string `json:"endpointBID"`
	EndpointA   string `json:"endpointA"`
	EndpointB   string `json:"endpointB"`
	Relay       string `json:"relay"`
	LastSuccess string `json:"lastSuccess"`
}

type ConnectionOverview struct {
	Revision string            `json:"revision"`
	Hosts    []ConnectionRow   `json:"hosts"`
	SOCKS    []ConnectionRow   `json:"socks"`
	Relays   []RelayCacheModel `json:"relays"`
}

type ConnectionTestRequest struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	TargetID string `json:"targetID"` // SOCKS test destination: a specific saved SSH route.
	Revision string `json:"revision"`
}

type ChallengeModel struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Secret    bool   `json:"secret,omitempty"`
	AllowSave bool   `json:"allowSave,omitempty"`
	AllowSkip bool   `json:"allowSkip,omitempty"`
}

type challengeAnswer struct {
	Accepted bool
	Value    string
	Save     bool
	Skipped  bool
}

type terminalDataModel struct {
	Session string `json:"session"`
	Pane    PaneID `json:"pane"`
	Data    string `json:"data"`
}

type terminalCWDModel struct {
	Sequence uint64 `json:"sequence"`
	Session  string `json:"session"`
	Pane     PaneID `json:"pane"`
	Path     string `json:"path"`
}

func timestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
