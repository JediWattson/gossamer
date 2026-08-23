// Package memoryprofile records engine-neutral browser memory checkpoints and
// optional Go heap profiles without changing Page or RegionStore ownership.
package memoryprofile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

type ProcessStats struct {
	HeapAlloc       uint64  `json:"heapAlloc"`
	HeapInuse       uint64  `json:"heapInuse"`
	HeapIdle        uint64  `json:"heapIdle"`
	HeapReleased    uint64  `json:"heapReleased"`
	HeapObjects     uint64  `json:"heapObjects"`
	StackInuse      uint64  `json:"stackInuse"`
	MSpanInuse      uint64  `json:"mspanInuse"`
	MCacheInuse     uint64  `json:"mcacheInuse"`
	NextGC          uint64  `json:"nextGC"`
	Sys             uint64  `json:"sys"`
	NumGC           uint32  `json:"numGC"`
	PauseTotalNanos uint64  `json:"pauseTotalNanos"`
	GCCPUFraction   float64 `json:"gcCPUFraction"`
}

type Snapshot struct {
	Sequence      uint64                      `json:"sequence"`
	Label         string                      `json:"label"`
	ElapsedNanos  int64                       `json:"elapsedNanos"`
	PageID        browser.PageID              `json:"pageId"`
	DocumentNodes int                         `json:"documentNodes"`
	LiveNodes     int                         `json:"liveNodes"`
	Process       ProcessStats                `json:"process"`
	Realm         browserruntime.RealmProfile `json:"realm"`
	Physical      memory.PhysicalStats        `json:"physical"`
	Regions       []memory.RegionCensus       `json:"regions,omitempty"`
	Script        browser.ScriptMemoryProfile `json:"script"`
	Execution     *ExecutionSnapshot          `json:"execution,omitempty"`
}

// ExecutionSnapshot attributes memory while Strand is still inside one long
// bytecode call and ordinary browser task checkpoints cannot run.
type ExecutionSnapshot struct {
	TaskID           browserruntime.TaskID `json:"taskId"`
	RawInstructions  uint64                `json:"rawInstructions"`
	ControlSteps     uint64                `json:"controlSteps"`
	StackDepth       int                   `json:"stackDepth"`
	Function         memory.Ref            `json:"function"`
	FunctionName     string                `json:"functionName"`
	FunctionHash     string                `json:"functionHash"`
	Instruction      uint32                `json:"instruction"`
	Opcode           string                `json:"opcode"`
	ActiveRegion     memory.RegionCensus   `json:"activeRegion"`
	PersistentRegion *memory.RegionCensus  `json:"persistentRegion,omitempty"`
}

type Recorder struct {
	mutex    sync.Mutex
	file     *os.File
	encoder  *json.Encoder
	heapPath string
	interval time.Duration
	started  time.Time
	last     time.Time
	sequence uint64
	closed   bool
}

func New(jsonPath, heapPath string, interval time.Duration) (*Recorder, error) {
	if interval < 0 {
		return nil, fmt.Errorf("memoryprofile: negative interval %s", interval)
	}
	recorder := &Recorder{heapPath: heapPath, interval: interval, started: time.Now()}
	if jsonPath == "" {
		return recorder, nil
	}
	file, err := os.Create(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("memoryprofile: create timeline: %w", err)
	}
	recorder.file = file
	recorder.encoder = json.NewEncoder(file)
	return recorder, nil
}

// Record appends one JSON checkpoint. Non-forced observations are throttled
// by interval so a busy timer loop cannot turn profiling into the workload.
func (recorder *Recorder) Record(label string, page *browser.Page, force bool) error {
	if recorder == nil || recorder.encoder == nil {
		return nil
	}
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	if recorder.closed {
		return errors.New("memoryprofile: recorder is closed")
	}
	now := time.Now()
	if !force && !recorder.last.IsZero() && now.Sub(recorder.last) < recorder.interval {
		return nil
	}
	recorder.sequence++
	snapshot, err := Capture(page, recorder.sequence, label, now.Sub(recorder.started))
	if err != nil {
		return err
	}
	if err := recorder.encoder.Encode(snapshot); err != nil {
		return fmt.Errorf("memoryprofile: encode checkpoint: %w", err)
	}
	recorder.last = now
	return nil
}

// RecordExecution appends one sparse in-task sample. The caller invokes it at
// an interpreter safepoint, so it reads RegionStore and DOM counters directly
// without trying to re-enter the active script Realm.
func (recorder *Recorder) RecordExecution(page *browser.Page, context *browserruntime.TaskContext, sample browserruntime.ExecutionSample) error {
	if recorder == nil || recorder.encoder == nil || page == nil || context == nil || context.Realm == nil {
		return nil
	}
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	if recorder.closed {
		return errors.New("memoryprofile: recorder is closed")
	}
	now := time.Now()
	if !recorder.last.IsZero() && now.Sub(recorder.last) < recorder.interval {
		return nil
	}
	recorder.sequence++
	snapshot, err := CaptureExecution(page, context, sample, recorder.sequence, now.Sub(recorder.started))
	if err != nil {
		return err
	}
	if err := recorder.encoder.Encode(snapshot); err != nil {
		return fmt.Errorf("memoryprofile: encode execution checkpoint: %w", err)
	}
	recorder.last = now
	return nil
}

func Capture(page *browser.Page, sequence uint64, label string, elapsed time.Duration) (Snapshot, error) {
	if page == nil || page.Realm == nil {
		return Snapshot{}, fmt.Errorf("memoryprofile: nil page")
	}
	process := captureProcessStats()
	script, err := page.ScriptMemoryProfile()
	if err != nil {
		return Snapshot{}, fmt.Errorf("memoryprofile: script profile: %w", err)
	}
	document := page.Document()
	documentNodes := 0
	liveNodes := 0
	if document != nil && document.Store() != nil {
		documentNodes = document.Store().Len()
		liveNodes = document.Store().LiveLen()
	}
	physical := page.Realm.Store().PhysicalStats()
	return Snapshot{
		Sequence:      sequence,
		Label:         label,
		ElapsedNanos:  elapsed.Nanoseconds(),
		PageID:        page.ID,
		DocumentNodes: documentNodes,
		LiveNodes:     liveNodes,
		Process:       process,
		Realm:         page.Realm.Profile(),
		Physical:      physical,
		Regions:       page.Realm.Store().RegionCensuses(),
		Script:        script,
	}, nil
}

// CaptureExecution is the in-task counterpart to Capture. It deliberately
// avoids Page.ScriptMemoryProfile because Strand's Realm mutex is already held
// by the executing callback.
func CaptureExecution(page *browser.Page, context *browserruntime.TaskContext, sample browserruntime.ExecutionSample, sequence uint64, elapsed time.Duration) (Snapshot, error) {
	if page == nil || page.Realm == nil || context == nil || context.Realm == nil {
		return Snapshot{}, fmt.Errorf("memoryprofile: nil active execution")
	}
	store := context.Realm.Store()
	active, err := store.RegionCensus(context.MemoryRegion)
	if err != nil {
		return Snapshot{}, fmt.Errorf("memoryprofile: active region census: %w", err)
	}
	var persistent *memory.RegionCensus
	if region := context.IntrinsicsRegion(); region != 0 && region != context.MemoryRegion {
		value, censusErr := store.RegionCensus(region)
		if censusErr != nil {
			return Snapshot{}, fmt.Errorf("memoryprofile: persistent region census: %w", censusErr)
		}
		persistent = &value
	}
	documentNodes, liveNodes := documentCounts(page)
	stats := store.Stats()
	return Snapshot{
		Sequence:      sequence,
		Label:         "active-execution",
		ElapsedNanos:  elapsed.Nanoseconds(),
		PageID:        page.ID,
		DocumentNodes: documentNodes,
		LiveNodes:     liveNodes,
		Process:       captureProcessStats(),
		Realm:         page.Realm.Profile(),
		Physical:      store.PhysicalStats(),
		Script: browser.ScriptMemoryProfile{
			Engine: "strand", LiveValues: stats.LiveSlots, LiveWrappers: stats.LiveHostObjects,
		},
		Execution: &ExecutionSnapshot{
			TaskID: sample.TaskID, RawInstructions: sample.RawInstructions,
			ControlSteps: sample.ControlSteps, StackDepth: sample.StackDepth,
			Function: sample.Function, FunctionName: sample.FunctionName,
			FunctionHash: fmt.Sprintf("%016x", sample.FunctionHash),
			Instruction:  sample.Instruction, Opcode: sample.Opcode.String(),
			ActiveRegion: active, PersistentRegion: persistent,
		},
	}, nil
}

func documentCounts(page *browser.Page) (int, int) {
	if page == nil {
		return 0, 0
	}
	document := page.Document()
	if document == nil || document.Store() == nil {
		return 0, 0
	}
	return document.Store().Len(), document.Store().LiveLen()
}

func captureProcessStats() ProcessStats {
	var process runtime.MemStats
	runtime.ReadMemStats(&process)
	return ProcessStats{
		HeapAlloc: process.HeapAlloc, HeapInuse: process.HeapInuse,
		HeapIdle: process.HeapIdle, HeapReleased: process.HeapReleased,
		HeapObjects: process.HeapObjects, StackInuse: process.StackInuse,
		MSpanInuse: process.MSpanInuse, MCacheInuse: process.MCacheInuse,
		NextGC: process.NextGC, Sys: process.Sys, NumGC: process.NumGC,
		PauseTotalNanos: process.PauseTotalNs, GCCPUFraction: process.GCCPUFraction,
	}
}

// WriteHeapProfile forces one Go collection and writes the live process heap.
// It is intentionally explicit so callers choose whether the profile is taken
// before or after Page teardown.
func (recorder *Recorder) WriteHeapProfile() error {
	if recorder == nil || recorder.heapPath == "" {
		return nil
	}
	runtime.GC()
	file, err := os.Create(recorder.heapPath)
	if err != nil {
		return fmt.Errorf("memoryprofile: create Go heap profile: %w", err)
	}
	profileErr := pprof.WriteHeapProfile(file)
	closeErr := file.Close()
	if profileErr != nil || closeErr != nil {
		return fmt.Errorf("memoryprofile: write Go heap profile: %w", errors.Join(profileErr, closeErr))
	}
	return nil
}

func (recorder *Recorder) Close() error {
	if recorder == nil {
		return nil
	}
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	if recorder.closed {
		return nil
	}
	recorder.closed = true
	if recorder.file == nil {
		return nil
	}
	if err := recorder.file.Sync(); err != nil {
		_ = recorder.file.Close()
		return fmt.Errorf("memoryprofile: sync timeline: %w", err)
	}
	if err := recorder.file.Close(); err != nil {
		return fmt.Errorf("memoryprofile: close timeline: %w", err)
	}
	return nil
}
