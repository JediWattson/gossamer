//go:build v8 && cgo && darwin && arm64

package v8engine

/*
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/v8/v8/out.gn/gossamer-profile -lgossamer_v8_bridge
#cgo LDFLAGS: -Wl,-rpath,${SRCDIR}/../../third_party/v8/v8/out.gn/gossamer-profile
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"fmt"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/JediWattson/gossamer/internal/browser"
)

type Engine struct {
	mutex          sync.Mutex
	config         Config
	realms         map[*Realm]struct{}
	latest         *Realm
	created        uint64
	closed         uint64
	closedProfiles RealmProfile
	isClosed       bool
}

type Realm struct {
	mutex    sync.Mutex
	engine   *Engine
	pointer  *C.gossamer_v8_realm
	isClosed bool
}

func New(config Config) (*Engine, error) {
	if config.ICUDataPath == "" {
		config.ICUDataPath = os.Getenv("GOSSAMER_V8_ICU_DATA")
	}
	if config.ICUDataPath == "" {
		return nil, ErrICUDataRequired
	}
	if _, err := os.Stat(config.ICUDataPath); err != nil {
		return nil, fmt.Errorf("v8engine: ICU data: %w", err)
	}
	if config.SamplingInterval == 0 {
		config.SamplingInterval = DefaultSamplingInterval
	}

	icuPath := C.CString(config.ICUDataPath)
	defer C.free(unsafe.Pointer(icuPath))
	var failure *C.char
	if C.gossamer_v8_initialize(icuPath, &failure) == 0 {
		return nil, takeError(failure)
	}
	return &Engine{config: config, realms: make(map[*Realm]struct{})}, nil
}

func (engine *Engine) NewRealm() (browser.JSRealm, error) {
	if engine == nil {
		return nil, fmt.Errorf("v8engine: nil engine")
	}
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	if engine.isClosed {
		return nil, ErrEngineClosed
	}

	var failure *C.char
	pointer := C.gossamer_v8_realm_new(C.uint64_t(engine.config.SamplingInterval), &failure)
	if pointer == nil {
		return nil, takeError(failure)
	}
	realm := &Realm{engine: engine, pointer: pointer}
	engine.realms[realm] = struct{}{}
	engine.latest = realm
	engine.created++
	return realm, nil
}

func (engine *Engine) LatestRealm() (*Realm, bool) {
	if engine == nil {
		return nil, false
	}
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	_, live := engine.realms[engine.latest]
	return engine.latest, engine.latest != nil && live
}

func (engine *Engine) Version() string {
	if engine == nil {
		return ""
	}
	return C.GoString(C.gossamer_v8_version())
}

func (engine *Engine) Profile() EngineProfile {
	if engine == nil {
		return EngineProfile{}
	}
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	return EngineProfile{
		Version:       engine.Version(),
		RealmsCreated: engine.created,
		RealmsClosed:  engine.closed,
		ClosedRealms:  engine.closedProfiles,
	}
}

func (engine *Engine) Close() error {
	if engine == nil {
		return nil
	}
	engine.mutex.Lock()
	if engine.isClosed {
		engine.mutex.Unlock()
		return nil
	}
	engine.isClosed = true
	realms := make([]*Realm, 0, len(engine.realms))
	for realm := range engine.realms {
		realms = append(realms, realm)
	}
	engine.mutex.Unlock()

	var result error
	for _, realm := range realms {
		if err := realm.Close(); err != nil && result == nil {
			result = err
		}
	}
	return result
}

func (realm *Realm) Evaluate(_ browser.Host, source browser.ScriptSource) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.isClosed || realm.pointer == nil {
		return ErrRealmClosed
	}
	var failure *C.char
	var sourcePointer *C.char
	if len(source.Source) != 0 {
		sourcePointer = (*C.char)(unsafe.Pointer(unsafe.StringData(source.Source)))
	}
	var urlPointer *C.char
	if len(source.URL) != 0 {
		urlPointer = (*C.char)(unsafe.Pointer(unsafe.StringData(source.URL)))
	}
	if C.gossamer_v8_realm_evaluate(
		realm.pointer,
		sourcePointer,
		C.size_t(len(source.Source)),
		urlPointer,
		C.size_t(len(source.URL)),
		&failure,
	) == 0 {
		return takeError(failure)
	}
	return nil
}

func (realm *Realm) DispatchEvent(browser.Host, browser.InputEvent) error {
	return ErrBindingsUnavailable
}

func (realm *Realm) Invoke(browser.Host, browser.ValueHandle) error {
	return ErrBindingsUnavailable
}

func (realm *Realm) DrainMicrotasks(browser.Host) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.isClosed || realm.pointer == nil {
		return ErrRealmClosed
	}
	var failure *C.char
	if C.gossamer_v8_realm_drain_microtasks(realm.pointer, &failure) == 0 {
		return takeError(failure)
	}
	return nil
}

func (realm *Realm) CollectGarbage() error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.isClosed || realm.pointer == nil {
		return ErrRealmClosed
	}
	var failure *C.char
	if C.gossamer_v8_realm_collect_garbage(realm.pointer, &failure) == 0 {
		return takeError(failure)
	}
	return nil
}

func (realm *Realm) Profile() (RealmProfile, error) {
	if realm == nil {
		return RealmProfile{}, ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.isClosed || realm.pointer == nil {
		return RealmProfile{}, ErrRealmClosed
	}
	var native C.gossamer_v8_profile
	var failure *C.char
	if C.gossamer_v8_realm_profile(realm.pointer, &native, &failure) == 0 {
		return RealmProfile{}, takeError(failure)
	}
	return profileFromC(native), nil
}

func (realm *Realm) Close() error {
	if realm == nil {
		return nil
	}
	realm.mutex.Lock()
	if realm.isClosed {
		realm.mutex.Unlock()
		return nil
	}
	var native C.gossamer_v8_profile
	var failure *C.char
	profileOK := C.gossamer_v8_realm_profile(realm.pointer, &native, &failure) != 0
	profileErr := error(nil)
	if !profileOK {
		profileErr = takeError(failure)
	}
	pointer := realm.pointer
	realm.pointer = nil
	realm.isClosed = true
	realm.mutex.Unlock()

	C.gossamer_v8_realm_delete(pointer)
	engine := realm.engine
	if engine != nil {
		engine.mutex.Lock()
		delete(engine.realms, realm)
		engine.closed++
		if profileOK {
			addRealmProfile(&engine.closedProfiles, profileFromC(native))
		}
		engine.mutex.Unlock()
	}
	return profileErr
}

func takeError(failure *C.char) error {
	if failure == nil {
		return fmt.Errorf("v8engine: operation failed")
	}
	message := C.GoString(failure)
	C.gossamer_v8_error_free(failure)
	return fmt.Errorf("v8engine: %s", message)
}

func profileFromC(native C.gossamer_v8_profile) RealmProfile {
	return RealmProfile{
		Heap: HeapStatistics{
			TotalHeapSize:         uint64(native.heap.total_heap_size),
			TotalHeapExecutable:   uint64(native.heap.total_heap_executable),
			TotalPhysicalSize:     uint64(native.heap.total_physical_size),
			TotalAvailableSize:    uint64(native.heap.total_available_size),
			UsedHeapSize:          uint64(native.heap.used_heap_size),
			HeapSizeLimit:         uint64(native.heap.heap_size_limit),
			MallocedMemory:        uint64(native.heap.malloced_memory),
			ExternalMemory:        uint64(native.heap.external_memory),
			PeakMallocedMemory:    uint64(native.heap.peak_malloced_memory),
			NativeContexts:        uint64(native.heap.native_contexts),
			DetachedContexts:      uint64(native.heap.detached_contexts),
			GlobalHandlesSize:     uint64(native.heap.global_handles_size),
			UsedGlobalHandlesSize: uint64(native.heap.used_global_handles_size),
			TotalAllocatedBytes:   uint64(native.heap.total_allocated_bytes),
		},
		Sampling: SamplingProfile{
			Samples:      uint64(native.samples),
			LiveSamples:  uint64(native.live_samples),
			SampledBytes: uint64(native.sampled_bytes),
			LiveBytes:    uint64(native.live_bytes),
		},
		Evaluations:          uint64(native.evaluations),
		EvaluationTime:       time.Duration(native.evaluation_nanos),
		MicrotaskCheckpoints: uint64(native.microtask_checkpoints),
		ForegroundTasks:      uint64(native.foreground_tasks),
		GCPrologues:          uint64(native.gc_prologues),
		GCEpilogues:          uint64(native.gc_epilogues),
		MinorGCs:             uint64(native.minor_gcs),
		MajorGCs:             uint64(native.major_gcs),
		GCTime:               time.Duration(native.gc_nanos),
	}
}

var _ browser.Engine = (*Engine)(nil)
var _ browser.JSRealm = (*Realm)(nil)
