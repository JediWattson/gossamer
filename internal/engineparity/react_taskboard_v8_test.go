//go:build v8 && cgo && darwin && arm64

package engineparity

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/v8engine"
)

func TestStockV8BootsSplitReactTaskboardThroughNavigation(t *testing.T) {
	engine, err := v8engine.New(v8engine.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runSplitReactTaskboardParity(t, engine, func(page *browser.Page) error {
		realm, ok := engine.LatestRealm()
		if !ok {
			return v8engine.ErrRealmClosed
		}
		return realm.CollectGarbage(page)
	})
}
