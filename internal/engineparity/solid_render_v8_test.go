//go:build v8 && cgo && darwin && arm64

package engineparity

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/v8engine"
)

func TestStockV8RunsProductionSolidCounterLifecycle(t *testing.T) {
	engine, err := v8engine.New(v8engine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runProductionSolidCounterParity(t, engine)
}

func TestStockV8RunsProductionSolidParitySequence(t *testing.T) {
	engine, err := v8engine.New(v8engine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runProductionSolidParitySequence(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return v8engine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}

func TestStockV8BootsProductionSolidModuleThroughNavigation(t *testing.T) {
	engine, err := v8engine.New(v8engine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runProductionSolidNavigationParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return v8engine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}

func TestStockV8LinksLiveModuleGraphWithCycles(t *testing.T) {
	engine, err := v8engine.New(v8engine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runLiveModuleGraphParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return v8engine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}

func TestStockV8CachesModuleFailuresAndReleasesGraphs(t *testing.T) {
	engine, err := v8engine.New(v8engine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runModuleFailureCacheParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return v8engine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}

func TestStockV8InstantiatesCyclicModuleBindingsBeforeEvaluation(t *testing.T) {
	engine, err := v8engine.New(v8engine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runModuleInstantiationParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return v8engine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}
