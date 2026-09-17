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
	"sync/atomic"
	"time"

	appconfig "github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/vault"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type paneState struct {
	name       string
	path       string
	endpoint   endpoint.Endpoint
	generation uint64
}

type terminalSession struct {
	id        string
	pane      PaneID
	pty       endpoint.PTYSession
	cancel    context.CancelFunc
	writeMu   sync.Mutex
	startOnce sync.Once
	endpoint  endpoint.Endpoint
	ctx       context.Context
	busy      atomic.Bool
}

type App struct {
	ctx           context.Context
	vaultPath     string
	queue         *jobs.Queue
	lifecycleMu   sync.Mutex
	locking       bool
	generation    uint64
	sessionCtx    context.Context
	sessionCancel context.CancelFunc
	forwardDone   chan struct{}
	eventSink     func(string, any) // Set only by in-package integration tests.

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
		forwardDone:      make(chan struct{}),
		panes:            make(map[PaneID]*paneState),
		terminals:        make(map[string]*terminalSession),
		challenges:       make(map[string]chan challengeAnswer),
		runtimePasswords: make(map[string]map[int]string),
		sessionSSH:       make(map[string]bool),
	}
	go app.forwardJobUpdates(app.queue, app.forwardDone)
	return app
}

func (a *App) Startup(ctx context.Context) { a.mu.Lock(); a.ctx = ctx; a.mu.Unlock() }

func (a *App) Shutdown(context.Context) {
	_ = a.Lock()
}

func (a *App) emit(name string, value any) {
	a.mu.RLock()
	ctx, sink, locking := a.ctx, a.eventSink, a.locking
	a.mu.RUnlock()
	if locking && name != "locked" {
		return
	}
	if sink != nil {
		sink(name, value)
		return
	}
	if ctx != nil {
		runtime.EventsEmit(ctx, name, value)
	}
}

func (a *App) VaultStatus() (VaultStatusModel, error) {
	a.mu.RLock()
	unlocked := a.store != nil && !a.locking
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
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	if a.isUnlocked() {
		return a.Bootstrap()
	}
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
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
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
	if a.queue.Closed() {
		a.queue = jobs.New(128)
		a.forwardDone = make(chan struct{})
		go a.forwardJobUpdates(a.queue, a.forwardDone)
	}
	a.generation++
	a.locking = false
	a.sessionCtx, a.sessionCancel = context.WithCancel(context.Background())
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
	if a.store == nil || a.locking {
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
		history = append(history, HistoryEntryModel{ID: item.ID, Operation: a.redactKnownLocked(item.Operation), Success: item.Success, Message: a.redactKnownLocked(item.Message), FinishedAt: timestamp(item.FinishedAt)})
	}
	return BootstrapModel{Unlocked: true, Hosts: hosts, LeftEndpoint: leftEndpoint, RightEndpoint: rightEndpoint, LeftPath: leftPath, RightPath: rightPath, Theme: theme, History: history}, nil
}

func (a *App) SetTheme(theme string) error {
	if theme != "system" && theme != "light" && theme != "dark" {
		return fmt.Errorf("未知主题 %q", theme)
	}
	a.mu.Lock()
	if a.store == nil || a.locking {
		a.mu.Unlock()
		return errors.New("保险库尚未解锁")
	}
	a.document.UI.Theme = theme
	a.mu.Unlock()
	a.requestSave()
	return nil
}

// Lock cancels admission and work before persisting the final queue snapshot.
// A fresh unlock gets a new queue; an old session can never write new history.
func (a *App) Lock() error {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	a.mu.Lock()
	a.locking = true
	a.generation++
	if a.sessionCancel != nil {
		a.sessionCancel()
	}
	queue, forwardDone := a.queue, a.forwardDone
	terminals, challenges := a.terminals, a.challenges
	a.terminals = make(map[string]*terminalSession)
	a.challenges = make(map[string]chan challengeAnswer)
	var endpoints []endpoint.Endpoint
	for _, pane := range a.panes {
		if pane.endpoint != nil {
			endpoints = append(endpoints, pane.endpoint)
		}
	}
	endpoints = append(endpoints, a.retired...)
	a.mu.Unlock()
	queue.Close()
	for _, response := range challenges {
		select {
		case response <- challengeAnswer{}:
		default:
		}
	}
	for _, session := range terminals {
		session.cancel()
		_ = session.pty.Close()
	}
	closeEndpoints := func() {
		seen := make(map[endpoint.Endpoint]bool)
		for _, item := range endpoints {
			if !seen[item] {
				seen[item] = true
				_ = item.Close()
			}
		}
	}
	var stopErr error
	select {
	case <-queue.Done():
	case <-time.After(10 * time.Second):
		// A stuck SFTP read may need its underlying connection closed.
		closeEndpoints()
		select {
		case <-queue.Done():
		case <-time.After(2 * time.Second):
			stopErr = errors.New("任务取消超时；保险库已锁定，远端清理需检查")
		}
	}
	if stopErr == nil {
		select {
		case <-forwardDone:
		case <-time.After(time.Second):
		}
	}
	for _, update := range queue.Snapshot() {
		if update.State == jobs.Running || update.State == jobs.Pending {
			update.State, update.FinishedAt, update.Message = jobs.Cancelled, time.Now(), "锁定时中断；请检查远端清理"
		}
		a.recordHistory(queue, update)
	}
	err := a.flushSave()
	a.mu.Lock()
	for index := range a.password {
		a.password[index] = 0
	}
	a.password, a.store = nil, nil
	a.document = appconfig.Document{}
	a.panes = make(map[PaneID]*paneState)
	a.retired = nil
	a.runtimePasswords = make(map[string]map[int]string)
	a.sessionSSH = make(map[string]bool)
	a.mu.Unlock()
	closeEndpoints()
	a.emit("locked", map[string]any{})
	return errors.Join(stopErr, err)
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
	// Keep the first deadline; activity must not postpone persistence forever.
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

func (a *App) jobModel(update jobs.Update) JobUpdateModel {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.jobModelLocked(update)
}

func (a *App) jobModelLocked(update jobs.Update) JobUpdateModel {
	return JobUpdateModel{ID: update.ID, Revision: update.Revision, State: string(update.State), Description: a.redactKnownLocked(update.Description), Message: a.redactKnownLocked(update.Message), Progress: update.Progress, ProgressKnown: update.ProgressKnown, Indeterminate: update.Indeterminate, Stage: update.Stage, Method: update.Method, BytesDone: update.BytesDone, BytesTotal: update.BytesTotal, FilesDone: update.FilesDone, FilesTotal: update.FilesTotal, StartedAt: timestamp(update.StartedAt), FinishedAt: timestamp(update.FinishedAt)}
}

func (a *App) recordHistory(queue *jobs.Queue, update jobs.Update) {
	if update.State != jobs.Succeeded && update.State != jobs.Failed && update.State != jobs.Cancelled {
		return
	}
	model := a.jobModel(update)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store == nil || queue != a.queue {
		return
	}
	for _, item := range a.document.History {
		if item.ID == update.ID {
			return
		}
	}
	a.document.History = append(a.document.History, appconfig.HistoryEntry{ID: update.ID, StartedAt: update.StartedAt, FinishedAt: update.FinishedAt, Operation: model.Description, Success: update.State == jobs.Succeeded, Message: model.Message})
	if len(a.document.History) > 500 {
		a.document.History = append([]appconfig.HistoryEntry(nil), a.document.History[len(a.document.History)-500:]...)
	}
}

func (a *App) forwardJobUpdates(queue *jobs.Queue, done chan struct{}) {
	defer close(done)
	for update := range queue.Updates() {
		a.mu.RLock()
		current := queue == a.queue
		a.mu.RUnlock()
		if !current {
			continue
		}
		a.recordHistory(queue, update)
		a.emit("job:update", a.jobModel(update))
		if update.State == jobs.Succeeded || update.State == jobs.Failed || update.State == jobs.Cancelled {
			a.requestSave()
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
	if a.store == nil || a.locking {
		a.mu.RUnlock()
		return nil, errors.New("保险库尚未解锁")
	}
	pane := a.panes[value]
	if pane != nil {
		copy := *pane
		copy.generation = a.generation
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
	privateKeyPattern       = regexp.MustCompile(`(?s)-----BEGIN (?:OPENSSH |RSA |EC |DSA |ENCRYPTED )?PRIVATE KEY-----.*?-----END (?:OPENSSH |RSA |EC |DSA |ENCRYPTED )?PRIVATE KEY-----`)
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
