package webgui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	appconfig "github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/vault"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type paneState struct {
	name     string
	path     string
	endpoint endpoint.Endpoint
}

type terminalSession struct {
	id      string
	pane    PaneID
	pty     endpoint.PTYSession
	cancel  context.CancelFunc
	writeMu sync.Mutex
}

type App struct {
	ctx       context.Context
	vaultPath string
	queue     *jobs.Queue

	mu               sync.RWMutex
	saveMu           sync.Mutex
	persistMu        sync.Mutex
	persistTimer     *time.Timer
	store            *vault.Store
	password         []byte
	document         appconfig.Document
	panes            map[PaneID]*paneState
	retired          []endpoint.Endpoint
	terminals        map[string]*terminalSession
	challenges       map[string]chan challengeAnswer
	runtimePasswords map[string]map[int]string
	sessionSSH       map[string]bool
	sequence         uint64
}

func New(vaultPath string) *App {
	app := &App{
		vaultPath:        vaultPath,
		queue:            jobs.New(128),
		panes:            make(map[PaneID]*paneState),
		terminals:        make(map[string]*terminalSession),
		challenges:       make(map[string]chan challengeAnswer),
		runtimePasswords: make(map[string]map[int]string),
		sessionSSH:       make(map[string]bool),
	}
	go app.forwardJobUpdates()
	return app
}

func (a *App) Startup(ctx context.Context) { a.ctx = ctx }

func (a *App) Shutdown(context.Context) {
	a.Lock()
	a.queue.Close()
}

func (a *App) emit(name string, value any) {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, name, value)
	}
}

func (a *App) VaultStatus() (VaultStatusModel, error) {
	a.mu.RLock()
	unlocked := a.store != nil
	a.mu.RUnlock()
	header, err := vault.ReadHeader(a.vaultPath)
	if errors.Is(err, os.ErrNotExist) {
		return VaultStatusModel{Unlocked: unlocked}, nil
	}
	if err != nil {
		return VaultStatusModel{}, err
	}
	return VaultStatusModel{Exists: true, Hint: header.Hint, Unlocked: unlocked}, nil
}

func (a *App) Unlock(password string) (BootstrapModel, error) {
	if password == "" {
		return BootstrapModel{}, errors.New("请输入主密码")
	}
	store, document, err := vault.Open(a.vaultPath, []byte(password))
	if err != nil {
		return BootstrapModel{}, err
	}
	a.installVault(store, []byte(password), document)
	return a.Bootstrap()
}

func (a *App) CreateVault(hint, password, confirm string) (BootstrapModel, error) {
	if password != confirm {
		return BootstrapModel{}, errors.New("两次输入的主密码不一致")
	}
	if _, err := os.Stat(a.vaultPath); err == nil {
		return BootstrapModel{}, errors.New("保险库已经存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return BootstrapModel{}, err
	}
	document := appconfig.NewDocument()
	store, err := vault.Create(a.vaultPath, strings.TrimSpace(hint), []byte(password), document)
	if err != nil {
		return BootstrapModel{}, err
	}
	a.installVault(store, []byte(password), document)
	return a.Bootstrap()
}

func (a *App) installVault(store *vault.Store, password []byte, document appconfig.Document) {
	local := endpoint.NewLocal()
	home, _ := local.Home(context.Background())
	leftPath, rightPath := document.UI.LeftPath, document.UI.RightPath
	if leftPath == "" {
		leftPath = home
	}
	if rightPath == "" {
		rightPath = home
	}
	a.mu.Lock()
	a.store = store
	a.password = append(a.password[:0], password...)
	a.document = document
	a.panes[LeftPane] = &paneState{name: "本机", path: leftPath, endpoint: local}
	a.panes[RightPane] = &paneState{name: "本机", path: rightPath, endpoint: endpoint.NewLocal()}
	a.mu.Unlock()
}

func (a *App) Bootstrap() (BootstrapModel, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store == nil {
		return BootstrapModel{}, errors.New("保险库尚未解锁")
	}
	hosts := []string{"本机"}
	for _, host := range a.document.Hosts {
		if !host.Disabled {
			hosts = append(hosts, host.Name)
		}
	}
	sort.Strings(hosts[1:])
	leftEndpoint := a.document.UI.LeftEndpoint
	rightEndpoint := a.document.UI.RightEndpoint
	if !contains(hosts, leftEndpoint) {
		leftEndpoint = "本机"
	}
	if !contains(hosts, rightEndpoint) {
		rightEndpoint = "本机"
	}
	leftPath, rightPath := a.document.UI.LeftPath, a.document.UI.RightPath
	if leftPath == "" {
		leftPath = a.panes[LeftPane].path
	}
	if rightPath == "" {
		rightPath = a.panes[RightPane].path
	}
	theme := a.document.UI.Theme
	if theme != "light" && theme != "dark" {
		theme = "system"
	}
	history := make([]HistoryEntryModel, 0, len(a.document.History))
	for _, item := range a.document.History {
		history = append(history, HistoryEntryModel{ID: item.ID, Operation: redact(item.Operation), Success: item.Success, Message: redact(item.Message), FinishedAt: timestamp(item.FinishedAt)})
	}
	return BootstrapModel{Unlocked: true, Hosts: hosts, LeftEndpoint: leftEndpoint, RightEndpoint: rightEndpoint, LeftPath: leftPath, RightPath: rightPath, Theme: theme, History: history}, nil
}

func (a *App) SetTheme(theme string) error {
	if theme != "system" && theme != "light" && theme != "dark" {
		return fmt.Errorf("未知主题 %q", theme)
	}
	a.mu.Lock()
	if a.store == nil {
		a.mu.Unlock()
		return errors.New("保险库尚未解锁")
	}
	a.document.UI.Theme = theme
	a.mu.Unlock()
	a.requestSave()
	return nil
}

func (a *App) Lock() {
	a.queue.CancelAll()
	_ = a.flushSave()
	a.mu.Lock()
	if a.store == nil {
		a.mu.Unlock()
		return
	}
	terminals := make([]*terminalSession, 0, len(a.terminals))
	for _, session := range a.terminals {
		terminals = append(terminals, session)
	}
	endpoints := make([]endpoint.Endpoint, 0, 2)
	for _, pane := range a.panes {
		if pane.endpoint != nil {
			endpoints = append(endpoints, pane.endpoint)
		}
	}
	endpoints = append(endpoints, a.retired...)
	for index := range a.password {
		a.password[index] = 0
	}
	a.password = nil
	a.store = nil
	a.document = appconfig.Document{}
	a.panes = make(map[PaneID]*paneState)
	a.retired = nil
	a.terminals = make(map[string]*terminalSession)
	a.runtimePasswords = make(map[string]map[int]string)
	a.sessionSSH = make(map[string]bool)
	a.mu.Unlock()
	for _, session := range terminals {
		session.cancel()
		_ = session.pty.Close()
	}
	seen := make(map[endpoint.Endpoint]bool)
	for _, item := range endpoints {
		if !seen[item] {
			seen[item] = true
			_ = item.Close()
		}
	}
	a.emit("locked", map[string]any{})
}

func (a *App) save() error {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store == nil {
		return errors.New("保险库尚未解锁")
	}
	store := a.store
	password := append([]byte(nil), a.password...)
	document := a.document
	defer func() {
		for index := range password {
			password[index] = 0
		}
	}()
	return store.Save(password, document)
}

func (a *App) requestSave() {
	a.persistMu.Lock()
	defer a.persistMu.Unlock()
	if a.persistTimer == nil {
		a.persistTimer = time.AfterFunc(2*time.Second, func() {
			a.persistMu.Lock()
			a.persistTimer = nil
			a.persistMu.Unlock()
			_ = a.save()
		})
		return
	}
	a.persistTimer.Reset(2 * time.Second)
}

func (a *App) flushSave() error {
	a.persistMu.Lock()
	if a.persistTimer != nil {
		a.persistTimer.Stop()
		a.persistTimer = nil
	}
	a.persistMu.Unlock()
	a.mu.RLock()
	unlocked := a.store != nil
	a.mu.RUnlock()
	if !unlocked {
		return nil
	}
	return a.save()
}

func (a *App) forwardJobUpdates() {
	for update := range a.queue.Updates() {
		model := JobUpdateModel{ID: update.ID, State: string(update.State), Description: redact(update.Description), Message: redact(update.Message), Progress: update.Progress, ProgressKnown: update.ProgressKnown, Indeterminate: update.Indeterminate, Stage: update.Stage, Method: update.Method, BytesDone: update.BytesDone, BytesTotal: update.BytesTotal, FilesDone: update.FilesDone, FilesTotal: update.FilesTotal, StartedAt: timestamp(update.StartedAt), FinishedAt: timestamp(update.FinishedAt)}
		a.emit("job:update", model)
		if update.State == jobs.Succeeded || update.State == jobs.Failed || update.State == jobs.Cancelled {
			a.mu.Lock()
			if a.store != nil {
				a.document.History = append(a.document.History, appconfig.HistoryEntry{ID: update.ID, StartedAt: update.StartedAt, FinishedAt: update.FinishedAt, Operation: redact(update.Description), Success: update.State == jobs.Succeeded, Message: redact(update.Message)})
				if len(a.document.History) > 500 {
					a.document.History = append([]appconfig.HistoryEntry(nil), a.document.History[len(a.document.History)-500:]...)
				}
			}
			a.mu.Unlock()
			_ = a.save()
		}
	}
}

func (a *App) nextID(prefix string) string {
	a.mu.Lock()
	a.sequence++
	id := fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixMilli(), a.sequence)
	a.mu.Unlock()
	return id
}

func (a *App) pane(value PaneID) (*paneState, error) {
	if !validPane(value) {
		return nil, fmt.Errorf("未知文件栏 %q", value)
	}
	a.mu.RLock()
	pane := a.panes[value]
	if pane != nil {
		copy := *pane
		pane = &copy
	}
	a.mu.RUnlock()
	if pane == nil || pane.endpoint == nil {
		return nil, errors.New("文件栏尚未初始化")
	}
	return pane, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

var (
	inlineCredentialPattern = regexp.MustCompile(`([[:alnum:]_.~%+-]+:)(?:"(?:\\.|[^"])*"|'(?:\\.|[^'])*'|[^\s@]+)(@)`)
	metadataSecretPattern   = regexp.MustCompile(`(?mi)^(###(?:sudo密码|root密码|口令)[ \t]*\r?\n)[^\r\n]+`)
	assignmentSecretPattern = regexp.MustCompile(`(?i)(\b(?:password|passwd|passphrase|sudo_password|root_password)\s*[:=]\s*)(?:"(?:\\.|[^"])*"|'(?:\\.|[^'])*'|[^\s,;]+)`)
	privateKeyPattern       = regexp.MustCompile(`(?s)-----BEGIN (?:OPENSSH |RSA |EC |DSA )?PRIVATE KEY-----.*?-----END (?:OPENSSH |RSA |EC |DSA )?PRIVATE KEY-----`)
)

// redact preserves all whitespace and line boundaries while stripping every
// credential form accepted by the Markdown editor and route parser before a
// value reaches UI logs or persistent history.
func redact(value string) string {
	value = privateKeyPattern.ReplaceAllString(value, "-----BEGIN PRIVATE KEY-----\n[REDACTED]\n-----END PRIVATE KEY-----")
	value = metadataSecretPattern.ReplaceAllString(value, "${1}***")
	value = assignmentSecretPattern.ReplaceAllString(value, "${1}***")
	return inlineCredentialPattern.ReplaceAllString(value, "${1}***${2}")
}
