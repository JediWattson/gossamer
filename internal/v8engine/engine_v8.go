//go:build v8 && cgo && darwin && arm64

package v8engine

/*
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/v8/v8/out.gn/gossamer-profile -lgossamer_v8_bridge
#cgo LDFLAGS: -Wl,-rpath,${SRCDIR}/../../third_party/v8/v8/out.gn/gossamer-profile
#include <stdlib.h>
#include "host_callbacks.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
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

func (realm *Realm) Evaluate(host browser.Host, source browser.ScriptSource) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.isClosed || realm.pointer == nil {
		return ErrRealmClosed
	}
	if err := realm.drainCollectedWrappersLocked(host); err != nil {
		return err
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
	executionID := registerHostExecution(host)
	defer unregisterHostExecution(executionID)
	var operationErr error
	if C.gossamer_v8_go_realm_evaluate(
		realm.pointer,
		C.uint64_t(executionID),
		sourcePointer,
		C.size_t(len(source.Source)),
		urlPointer,
		C.size_t(len(source.URL)),
		&failure,
	) == 0 {
		operationErr = takeError(failure)
	}
	return errors.Join(operationErr, realm.drainCollectedWrappersLocked(host))
}

func (realm *Realm) DispatchEvent(host browser.Host, event browser.InputEvent) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.isClosed || realm.pointer == nil {
		return ErrRealmClosed
	}
	if err := realm.drainCollectedWrappersLocked(host); err != nil {
		return err
	}
	executionID := registerHostExecution(host)
	defer unregisterHostExecution(executionID)
	stringData := func(value string) *C.char {
		if value == "" {
			return nil
		}
		return (*C.char)(unsafe.Pointer(unsafe.StringData(value)))
	}
	native := C.gossamer_v8_input_event{
		_type:               C.uint8_t(event.Type),
		document:            C.uint64_t(event.Target.Document),
		node:                C.uint32_t(event.Target.Node),
		related_document:    C.uint64_t(event.RelatedTarget.Document),
		related_node:        C.uint32_t(event.RelatedTarget.Node),
		x:                   C.double(event.X),
		y:                   C.double(event.Y),
		button:              C.int32_t(event.Button),
		buttons:             C.uint32_t(event.Buttons),
		pointer_id:          C.int32_t(event.PointerID),
		pointer_type:        stringData(event.PointerType),
		pointer_type_length: C.size_t(len(event.PointerType)),
		key:                 stringData(event.Key),
		key_length:          C.size_t(len(event.Key)),
		code:                stringData(event.Code),
		code_length:         C.size_t(len(event.Code)),
		data:                stringData(event.Data),
		data_length:         C.size_t(len(event.Data)),
		input_type:          stringData(event.InputType),
		input_type_length:   C.size_t(len(event.InputType)),
	}
	if event.IsPrimary {
		native.is_primary = 1
	}
	if event.Repeat {
		native.repeat = 1
	}
	if event.IsComposing {
		native.is_composing = 1
	}
	if event.AltKey {
		native.alt_key = 1
	}
	if event.CtrlKey {
		native.ctrl_key = 1
	}
	if event.MetaKey {
		native.meta_key = 1
	}
	if event.ShiftKey {
		native.shift_key = 1
	}
	var failure *C.char
	var operationErr error
	if C.gossamer_v8_go_realm_dispatch_event(
		realm.pointer,
		C.uint64_t(executionID),
		&native,
		&failure,
	) == 0 {
		operationErr = takeError(failure)
	}
	return errors.Join(operationErr, realm.drainCollectedWrappersLocked(host))
}

func (realm *Realm) Invoke(host browser.Host, callback browser.ValueHandle) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.isClosed || realm.pointer == nil {
		return ErrRealmClosed
	}
	if err := realm.drainCollectedWrappersLocked(host); err != nil {
		return err
	}
	executionID := registerHostExecution(host)
	defer unregisterHostExecution(executionID)
	var failure *C.char
	var operationErr error
	if C.gossamer_v8_go_realm_invoke(
		realm.pointer,
		C.uint64_t(executionID),
		C.uint64_t(callback),
		&failure,
	) == 0 {
		operationErr = takeError(failure)
	}
	return errors.Join(operationErr, realm.drainCollectedWrappersLocked(host))
}

func (realm *Realm) DrainMicrotasks(host browser.Host) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.isClosed || realm.pointer == nil {
		return ErrRealmClosed
	}
	if err := realm.drainCollectedWrappersLocked(host); err != nil {
		return err
	}
	executionID := registerHostExecution(host)
	defer unregisterHostExecution(executionID)
	var failure *C.char
	var operationErr error
	if C.gossamer_v8_go_realm_drain_microtasks(
		realm.pointer,
		C.uint64_t(executionID),
		&failure,
	) == 0 {
		operationErr = takeError(failure)
	}
	return errors.Join(operationErr, realm.drainCollectedWrappersLocked(host))
}

func (realm *Realm) CollectGarbage(lifetimes ...browser.NodeWrapperLifetimeHost) error {
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
	if len(lifetimes) != 0 {
		return realm.drainCollectedWrappersLocked(lifetimes[0])
	}
	return nil
}

func (realm *Realm) drainCollectedWrappersLocked(host any) error {
	count := int(C.gossamer_v8_realm_take_collected_wrappers(realm.pointer, nil, 0))
	if count == 0 {
		return nil
	}
	lifetimes, ok := host.(browser.NodeWrapperLifetimeHost)
	if !ok {
		return fmt.Errorf("v8engine: host cannot release %d collected DOM wrappers", count)
	}
	native := make([]C.gossamer_v8_node_handle, count)
	taken := int(C.gossamer_v8_realm_take_collected_wrappers(
		realm.pointer,
		(*C.gossamer_v8_node_handle)(unsafe.Pointer(&native[0])),
		C.size_t(len(native)),
	))
	handles := make([]browser.NodeHandle, 0, taken)
	for _, handle := range native[:taken] {
		handles = append(handles, browser.NodeHandle{
			Document: browser.DocumentGeneration(handle.document),
			Node:     dom.NodeID(handle.node),
		})
	}
	return lifetimes.ReleaseNodeWrappers(handles)
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
		WrappersCreated:      uint64(native.wrappers_created),
		WrapperCacheHits:     uint64(native.wrapper_cache_hits),
		WrappersCollected:    uint64(native.wrappers_collected),
		LiveWrappers:         uint64(native.live_wrappers),
		CallbacksCreated:     uint64(native.callbacks_created),
		CallbacksInvoked:     uint64(native.callbacks_invoked),
		LiveCallbacks:        uint64(native.live_callbacks),
		EventListeners:       uint64(native.event_listeners),
		EventsDispatched:     uint64(native.events_dispatched),
	}
}

var _ browser.Engine = (*Engine)(nil)
var _ browser.JSRealm = (*Realm)(nil)
