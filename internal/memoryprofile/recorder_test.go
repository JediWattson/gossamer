package memoryprofile_test

import (
	"bufio"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/memoryprofile"
	"github.com/JediWattson/gossamer/internal/nativeengine"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestRecorderWritesEngineNeutralPhysicalTimeline(t *testing.T) {
	browserRuntime, err := browser.NewWithEngine(nativeengine.New(nativeengine.Config{}))
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.NewPage(dom.NewDocument(), &url.URL{Scheme: "https", Host: "profile.test"})
	if err != nil {
		t.Fatal(err)
	}
	baselineEdges := page.Realm.Ledger().PhysicalStats().LiveEdgeEntries
	owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: 77_001}
	region, err := page.Realm.Ledger().CreateRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	from, err := page.Realm.Ledger().CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	to, err := page.Realm.Ledger().CreateObject(region)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.Ledger().AddReference(from, to); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "timeline.jsonl")
	recorder, err := memoryprofile.New(path, "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Record("loaded", page, true); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Record("throttled", page, false); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Record("final", page, true); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var snapshots []memoryprofile.Snapshot
	for scanner.Scan() {
		var snapshot memoryprofile.Snapshot
		if err := json.Unmarshal(scanner.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || snapshots[0].Label != "loaded" || snapshots[1].Label != "final" {
		t.Fatalf("timeline = %#v", snapshots)
	}
	if snapshots[0].Process.HeapInuse == 0 || snapshots[0].Physical.SlotSizeBytes == 0 || snapshots[0].DocumentNodes == 0 {
		t.Fatalf("initial snapshot omitted physical counters: %#v", snapshots[0])
	}
	if snapshots[0].Script.Engine != "strand" || snapshots[0].Script.HeapBytes != 0 {
		t.Fatalf("Strand heap scope is not explicit: %#v", snapshots[0].Script)
	}
	if snapshots[0].Physical.AttributedBytes == 0 || snapshots[0].Realm.OwnershipPhysical.LiveEdgeEntries != baselineEdges+1 ||
		snapshots[0].Realm.OwnershipPhysical.EdgeArenaReservedBytes == 0 {
		t.Fatalf("initial snapshot omitted attributed heap gauges: %#v", snapshots[0])
	}
}
