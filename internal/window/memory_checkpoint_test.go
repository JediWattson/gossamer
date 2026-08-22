package window

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
)

type checkpointTestEngine struct{ realm *checkpointTestRealm }

func (engine *checkpointTestEngine) NewRealm() (browser.JSRealm, error) { return engine.realm, nil }
func (*checkpointTestEngine) Close() error                              { return nil }

type checkpointTestRealm struct{ checkpoints int }

func (*checkpointTestRealm) Evaluate(browser.Host, browser.ScriptSource) error { return nil }
func (*checkpointTestRealm) DispatchEvent(browser.Host, browser.InputEvent) (browser.EventDispatchResult, error) {
	return browser.EventDispatchResult{}, nil
}
func (*checkpointTestRealm) Invoke(browser.Host, browser.ValueHandle) error { return nil }
func (*checkpointTestRealm) DrainMicrotasks(browser.Host) error             { return nil }
func (*checkpointTestRealm) Close() error                                   { return nil }
func (realm *checkpointTestRealm) CollectCheckpoint(browser.NodeWrapperLifetimeHost) (bool, error) {
	realm.checkpoints++
	return true, nil
}

func TestPageTaskPumpRunsEngineMemoryCheckpointAfterDrain(t *testing.T) {
	realm := &checkpointTestRealm{}
	browserRuntime, err := browser.NewWithEngine(&checkpointTestEngine{realm: realm})
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.NewPage(dom.NewDocument(), &url.URL{Scheme: "https", Host: "checkpoint.test"})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := pumpPageTasks(context.Background(), page); err != nil || count != 0 {
		t.Fatalf("pumpPageTasks = %d, %v", count, err)
	}
	if realm.checkpoints != 1 {
		t.Fatalf("checkpoint calls = %d, want 1", realm.checkpoints)
	}
}

var _ browser.JSCheckpointCollector = (*checkpointTestRealm)(nil)
