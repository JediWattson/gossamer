package window

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const sessionSnapshotVersion = 1

// SessionSnapshot is the bounded browser-chrome state persisted between
// native-window runs. DOM, script, frame, and native object state are rebuilt
// through ordinary navigation and never serialized by the shell.
type SessionSnapshot struct {
	Version        int      `json:"version"`
	Tabs           []string `json:"tabs"`
	ActiveTab      int      `json:"activeTab"`
	InspectorOpen  bool     `json:"inspectorOpen"`
	InspectorPanel uint8    `json:"inspectorPanel"`
}

// SessionStore loads and saves copied Graphite session state.
type SessionStore interface {
	LoadSession(context.Context) (SessionSnapshot, error)
	SaveSession(context.Context, SessionSnapshot) error
}

// MemorySessionStore is a concurrency-safe deterministic session store for
// embedders and tests.
type MemorySessionStore struct {
	mutex    sync.Mutex
	snapshot SessionSnapshot
	saved    bool
}

func NewMemorySessionStore(snapshot SessionSnapshot) *MemorySessionStore {
	return &MemorySessionStore{snapshot: cloneSessionSnapshot(snapshot), saved: snapshot.Version != 0}
}

func (store *MemorySessionStore) LoadSession(ctx context.Context) (SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}
	if store == nil {
		return SessionSnapshot{}, nil
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if !store.saved {
		return SessionSnapshot{}, nil
	}
	return cloneSessionSnapshot(store.snapshot), nil
}

func (store *MemorySessionStore) SaveSession(ctx context.Context, snapshot SessionSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if store == nil {
		return fmt.Errorf("window: nil memory session store")
	}
	store.mutex.Lock()
	store.snapshot = cloneSessionSnapshot(snapshot)
	store.saved = true
	store.mutex.Unlock()
	return nil
}

func (store *MemorySessionStore) Snapshot() (SessionSnapshot, bool) {
	if store == nil {
		return SessionSnapshot{}, false
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return cloneSessionSnapshot(store.snapshot), store.saved
}

// FileSessionStore persists one JSON snapshot with an atomic same-directory
// rename. A missing file represents a fresh session.
type FileSessionStore struct {
	Path string
}

func (store FileSessionStore) LoadSession(ctx context.Context) (SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}
	if store.Path == "" {
		return SessionSnapshot{}, fmt.Errorf("window: empty session file path")
	}
	data, err := os.ReadFile(store.Path)
	if errors.Is(err, os.ErrNotExist) {
		return SessionSnapshot{}, nil
	}
	if err != nil {
		return SessionSnapshot{}, err
	}
	var snapshot SessionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return SessionSnapshot{}, fmt.Errorf("decode session: %w", err)
	}
	return snapshot, nil
}

func (store FileSessionStore) SaveSession(ctx context.Context, snapshot SessionSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if store.Path == "" {
		return fmt.Errorf("window: empty session file path")
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(store.Path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".gossamer-session-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, store.Path); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func cloneSessionSnapshot(snapshot SessionSnapshot) SessionSnapshot {
	snapshot.Tabs = append([]string(nil), snapshot.Tabs...)
	return snapshot
}

func (shell *graphiteShell) loadSession(ctx context.Context) error {
	if shell == nil || shell.sessionStore == nil {
		return nil
	}
	snapshot, err := shell.sessionStore.LoadSession(ctx)
	if err != nil {
		return err
	}
	if snapshot.Version == 0 {
		return nil
	}
	if snapshot.Version != sessionSnapshotVersion {
		return fmt.Errorf("window: unsupported session snapshot version %d", snapshot.Version)
	}
	if len(snapshot.Tabs) > maximumGraphiteTabs {
		snapshot.Tabs = append([]string(nil), snapshot.Tabs[:maximumGraphiteTabs]...)
	}
	if snapshot.ActiveTab < 0 || snapshot.ActiveTab >= len(snapshot.Tabs) {
		snapshot.ActiveTab = 0
	}
	if snapshot.InspectorPanel > uint8(inspectorMemory) {
		snapshot.InspectorPanel = uint8(inspectorDOM)
	}
	shell.pendingSession = &snapshot
	return nil
}

func (shell *graphiteShell) restoreSession(ctx context.Context) error {
	if shell == nil || shell.pendingSession == nil {
		return nil
	}
	snapshot := cloneSessionSnapshot(*shell.pendingSession)
	shell.pendingSession = nil
	shell.inspectorOpen = snapshot.InspectorOpen
	shell.inspectorPanel = inspectorPanel(snapshot.InspectorPanel)
	if len(snapshot.Tabs) == 0 {
		return nil
	}
	if snapshot.Tabs[0] != "" && snapshot.Tabs[0] != shell.address {
		if err := shell.navigate(ctx, shell.activePage(), snapshot.Tabs[0]); err != nil {
			return err
		}
	}
	for _, location := range snapshot.Tabs[1:] {
		before := len(shell.tabs)
		if err := shell.openTab(ctx); err != nil {
			return err
		}
		if len(shell.tabs) == before {
			break
		}
		if location != "" {
			if err := shell.navigate(ctx, shell.activePage(), location); err != nil {
				return err
			}
		}
	}
	if snapshot.ActiveTab >= 0 && snapshot.ActiveTab < len(shell.tabs) {
		shell.switchTab(snapshot.ActiveTab)
	}
	shell.revision++
	return nil
}

func (shell *graphiteShell) saveSession(ctx context.Context) error {
	if shell == nil || shell.sessionStore == nil {
		return nil
	}
	shell.saveActiveTab()
	snapshot := SessionSnapshot{
		Version:        sessionSnapshotVersion,
		ActiveTab:      shell.activeTab,
		InspectorOpen:  shell.inspectorOpen,
		InspectorPanel: uint8(shell.inspectorPanel),
		Tabs:           make([]string, 0, len(shell.tabs)),
	}
	for _, tab := range shell.tabs {
		location := tab.state.address
		if !tab.state.loading && tab.page != nil && tab.page.URL() != nil {
			location = tab.page.URL().String()
		}
		snapshot.Tabs = append(snapshot.Tabs, location)
	}
	return shell.sessionStore.SaveSession(ctx, snapshot)
}
