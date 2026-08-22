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
)

func TestRecorderWritesEngineNeutralPhysicalTimeline(t *testing.T) {
	browserRuntime, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.NewPage(dom.NewDocument(), &url.URL{Scheme: "https", Host: "profile.test"})
	if err != nil {
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
}
