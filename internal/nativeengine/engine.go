// Package nativeengine adapts Gossamer's RegionStore interpreter to the
// browser.Engine and browser.JSRealm boundaries. It does not call or modify V8.
package nativeengine

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/js/compiler"
	"github.com/JediWattson/gossamer/internal/js/program"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

var (
	ErrEngineClosed           = errors.New("nativeengine: engine is closed")
	ErrRealmClosed            = errors.New("nativeengine: realm is closed")
	ErrNativeTaskHost         = errors.New("nativeengine: host does not expose a runtime task")
	ErrRuntimeRealmChange     = errors.New("nativeengine: runtime Realm changed")
	ErrCheckpointRequired     = errors.New("nativeengine: previous task has not reached its microtask checkpoint")
	ErrUnknownValueHandle     = errors.New("nativeengine: unknown value handle")
	ErrModuleGraphUnsupported = errors.New("nativeengine: unsupported module graph")
	ErrModuleGraphInvalid     = errors.New("nativeengine: invalid module graph")
	ErrModuleLink             = errors.New("nativeengine: module link failed")
)

type Config struct {
	Interpreter                  browserruntime.InterpreterConfig
	CheckpointCollectionInterval uint64
	ConsoleSink                  func(ConsoleMessage)
}

type EngineProfile struct {
	RealmsCreated         uint64
	RealmsClosed          uint64
	LiveRealms            uint64
	ModuleCompilations    uint64
	Evaluations           uint64
	Checkpoints           uint64
	SourceBytes           uint64
	CheckpointCollections uint64
}

type RealmProfile struct {
	ModuleCompilations    uint64
	Evaluations           uint64
	Checkpoints           uint64
	SourceBytes           uint64
	PersistentRegion      memory.RegionID
	ActiveTask            browserruntime.TaskID
	Closed                bool
	CheckpointCollections uint64
}

type Engine struct {
	mutex         sync.Mutex
	config        Config
	realms        map[*Realm]struct{}
	latest        *Realm
	closed        bool
	created       uint64
	closedCount   uint64
	closedProfile RealmProfile
}

type Realm struct {
	mutex       sync.Mutex
	engine      *Engine
	interpreter *browserruntime.Interpreter
	runtime     *browserruntime.Realm

	persistent                   *browserruntime.Intrinsics
	persistentRegion             memory.RegionID
	persistentWrapperCache       memory.Ref
	persistentFacadeCache        memory.Ref
	persistentCollectionCache    memory.Ref
	persistentCallbackCache      memory.Ref
	persistentObserverCache      memory.Ref
	persistentModuleCache        memory.Ref
	active                       *browserruntime.Intrinsics
	activeRegion                 memory.RegionID
	activeTask                   browserruntime.TaskID
	bindings                     *browserBindings
	host                         browser.Host
	nextCallback                 browser.ValueHandle
	timerCallbacks               map[browser.TimerID]browser.ValueHandle
	intervalCallbacks            map[browser.TimerID]browser.ValueHandle
	intervalTimers               map[browser.TimerID]browser.TimerID
	callbackIntervals            map[browser.ValueHandle]browser.TimerID
	intervalDelays               map[browser.TimerID]time.Duration
	animationCallbacks           map[browser.AnimationFrameID]browser.ValueHandle
	mutationObservers            map[uint64]*mutationObserverState
	nextMutationObserver         uint64
	collectedWrappers            []browser.NodeHandle
	listeners                    map[eventListenerKey][]eventListener
	listenerTargets              map[eventTargetID]uint64
	activeEvent                  *eventState
	modules                      map[string]*nativeModule
	moduleResolutions            map[moduleResolutionKey]string
	checkpointCollectionInterval uint64
	lastCheckpointCollection     uint64
	checkpointCollections        uint64
	consoleSink                  func(ConsoleMessage)

	evaluations        uint64
	moduleCompilations uint64
	checkpoints        uint64
	sourceBytes        uint64
	closed             bool
}

func New(config Config) *Engine {
	return &Engine{config: config, realms: make(map[*Realm]struct{})}
}

func (engine *Engine) NewRealm() (browser.JSRealm, error) {
	if engine == nil {
		return nil, fmt.Errorf("nativeengine: nil engine")
	}
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	if engine.closed {
		return nil, ErrEngineClosed
	}
	collectionInterval := engine.config.CheckpointCollectionInterval
	if collectionInterval == 0 {
		collectionInterval = DefaultCheckpointCollectionInterval
	}
	realm := &Realm{
		engine:                       engine,
		interpreter:                  browserruntime.NewInterpreter(engine.config.Interpreter),
		timerCallbacks:               make(map[browser.TimerID]browser.ValueHandle),
		intervalCallbacks:            make(map[browser.TimerID]browser.ValueHandle),
		intervalTimers:               make(map[browser.TimerID]browser.TimerID),
		callbackIntervals:            make(map[browser.ValueHandle]browser.TimerID),
		intervalDelays:               make(map[browser.TimerID]time.Duration),
		animationCallbacks:           make(map[browser.AnimationFrameID]browser.ValueHandle),
		mutationObservers:            make(map[uint64]*mutationObserverState),
		listeners:                    make(map[eventListenerKey][]eventListener),
		listenerTargets:              make(map[eventTargetID]uint64),
		modules:                      make(map[string]*nativeModule),
		moduleResolutions:            make(map[moduleResolutionKey]string),
		checkpointCollectionInterval: collectionInterval,
		consoleSink:                  engine.config.ConsoleSink,
	}
	if err := realm.installBrowserNatives(); err != nil {
		return nil, err
	}
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

func (engine *Engine) Profile() EngineProfile {
	if engine == nil {
		return EngineProfile{}
	}
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	profile := EngineProfile{
		RealmsCreated:         engine.created,
		RealmsClosed:          engine.closedCount,
		LiveRealms:            uint64(len(engine.realms)),
		ModuleCompilations:    engine.closedProfile.ModuleCompilations,
		Evaluations:           engine.closedProfile.Evaluations,
		Checkpoints:           engine.closedProfile.Checkpoints,
		SourceBytes:           engine.closedProfile.SourceBytes,
		CheckpointCollections: engine.closedProfile.CheckpointCollections,
	}
	for realm := range engine.realms {
		realmProfile := realm.profile()
		profile.ModuleCompilations += realmProfile.ModuleCompilations
		profile.Evaluations += realmProfile.Evaluations
		profile.Checkpoints += realmProfile.Checkpoints
		profile.SourceBytes += realmProfile.SourceBytes
		profile.CheckpointCollections += realmProfile.CheckpointCollections
	}
	return profile
}

func (engine *Engine) Close() error {
	if engine == nil {
		return nil
	}
	engine.mutex.Lock()
	if engine.closed {
		engine.mutex.Unlock()
		return nil
	}
	engine.closed = true
	realms := make([]*Realm, 0, len(engine.realms))
	for realm := range engine.realms {
		realms = append(realms, realm)
	}
	engine.mutex.Unlock()

	var result error
	for _, realm := range realms {
		result = errors.Join(result, realm.Close())
	}
	return result
}

func (engine *Engine) forget(realm *Realm, profile RealmProfile) {
	if engine == nil || realm == nil {
		return
	}
	engine.mutex.Lock()
	if _, exists := engine.realms[realm]; exists {
		delete(engine.realms, realm)
		engine.closedCount++
		engine.closedProfile.ModuleCompilations += profile.ModuleCompilations
		engine.closedProfile.Evaluations += profile.Evaluations
		engine.closedProfile.Checkpoints += profile.Checkpoints
		engine.closedProfile.SourceBytes += profile.SourceBytes
	}
	engine.mutex.Unlock()
}

func (realm *Realm) Evaluate(host browser.Host, source browser.ScriptSource) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return ErrRealmClosed
	}
	realm.host = host
	defer func() { realm.host = nil }()
	return realm.evaluateLocked(host, source)
}

func (realm *Realm) evaluateLocked(host browser.Host, source browser.ScriptSource) error {
	realm.evaluations++
	realm.sourceBytes += uint64(len(source.Source))

	image, err := compiler.CompileWithOptions(source.Source, compiler.Options{AllowUnresolvedGlobals: true})
	if err != nil {
		return evaluationError(source.URL, err)
	}
	task, err := runtimeTask(host)
	if err != nil {
		return evaluationError(source.URL, err)
	}
	scope, err := realm.beginTaskLocked(task)
	if err != nil {
		return evaluationError(source.URL, err)
	}
	loaded, err := program.Load(scope, image, memory.RefValue(realm.active.Global))
	if err != nil {
		return evaluationError(source.URL, err)
	}
	_, err = realm.interpreter.ExecuteWithoutCheckpoint(scope, loaded.Entry)
	if err != nil {
		return evaluationError(source.URL, describeExecutionError(scope, err))
	}
	return nil
}

func describeExecutionError(context *browserruntime.TaskContext, err error) error {
	value, thrown := browserruntime.ThrownValue(err)
	if !thrown || context == nil || !value.IsRef() {
		return err
	}
	kind, kindErr := context.HeapKind(value.Ref())
	if kindErr != nil || kind != memory.HeapError {
		return err
	}
	object, objectErr := context.DerefError(value.Ref())
	if objectErr != nil || !object.Message.IsRef() {
		return err
	}
	message, messageErr := context.DerefString(object.Message.Ref())
	if messageErr != nil || message == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, message)
}

func (realm *Realm) DrainMicrotasks(host browser.Host) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return ErrRealmClosed
	}
	realm.host = host
	defer func() { realm.host = nil }()
	realm.checkpoints++
	if realm.activeTask == 0 {
		return nil
	}
	task, err := runtimeTask(host)
	if err != nil {
		return err
	}
	if task.TaskID != realm.activeTask {
		return fmt.Errorf("%w: active task %d, checkpoint task %d", ErrCheckpointRequired, realm.activeTask, task.TaskID)
	}
	scope, err := realm.activeScopeLocked(task)
	if err != nil {
		realm.interpreter.DiscardJobs(realm.activeTask)
		realm.clearActiveLocked()
		return err
	}
	observerErr := realm.deliverMutationObserversLocked(scope)
	if observerErr != nil {
		observerErr = fmt.Errorf("nativeengine: deliver initial mutation observers in task %d: %w", task.TaskID, observerErr)
	}
	jobErr := realm.interpreter.DrainJobs(scope)
	if jobErr != nil {
		jobErr = fmt.Errorf("nativeengine: drain initial jobs in task %d: %w", task.TaskID, jobErr)
	}
	if observerErr == nil && jobErr == nil {
		observerErr = realm.deliverMutationObserversLocked(scope)
		if observerErr != nil {
			observerErr = fmt.Errorf("nativeengine: deliver follow-up mutation observers in task %d: %w", task.TaskID, observerErr)
		}
	}
	if observerErr == nil && jobErr == nil {
		jobErr = realm.interpreter.DrainJobs(scope)
		if jobErr != nil {
			jobErr = fmt.Errorf("nativeengine: drain follow-up jobs in task %d: %w", task.TaskID, jobErr)
		}
	}
	persistErr := realm.persistLocked(task)
	if persistErr != nil {
		persistErr = fmt.Errorf("nativeengine: persist task %d: %w", task.TaskID, persistErr)
	}
	return errors.Join(jobErr, observerErr, persistErr)
}

func (realm *Realm) DispatchEvent(host browser.Host, event browser.InputEvent) (browser.EventDispatchResult, error) {
	if realm == nil {
		return browser.EventDispatchResult{}, ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return browser.EventDispatchResult{}, ErrRealmClosed
	}
	realm.host = host
	defer func() { realm.host = nil }()
	task, err := runtimeTask(host)
	if err != nil {
		return browser.EventDispatchResult{}, err
	}
	scope, err := realm.beginTaskLocked(task)
	if err != nil {
		return browser.EventDispatchResult{}, err
	}
	return realm.dispatchEventLocked(scope, event)
}

func (realm *Realm) Invoke(host browser.Host, handle browser.ValueHandle) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return ErrRealmClosed
	}
	realm.host = host
	defer func() { realm.host = nil }()
	task, err := runtimeTask(host)
	if err != nil {
		return err
	}
	scope, err := realm.beginTaskLocked(task)
	if err != nil {
		return fmt.Errorf("nativeengine: begin callback %d in task %d: %w", handle, task.TaskID, err)
	}
	if err := realm.invokeCallbackLocked(scope, handle); err != nil {
		return fmt.Errorf("nativeengine: invoke callback %d in task %d: %w", handle, task.TaskID, err)
	}
	return nil
}

func (realm *Realm) InvokeAnimationFrame(host browser.Host, handle browser.ValueHandle, timestamp float64) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return ErrRealmClosed
	}
	realm.host = host
	defer func() { realm.host = nil }()
	task, err := runtimeTask(host)
	if err != nil {
		return err
	}
	scope, err := realm.beginTaskLocked(task)
	if err != nil {
		return fmt.Errorf("nativeengine: begin animation callback %d in task %d: %w", handle, task.TaskID, err)
	}
	for frame, callbackHandle := range realm.animationCallbacks {
		if callbackHandle == handle {
			delete(realm.animationCallbacks, frame)
			break
		}
	}
	if err := realm.invokeCallbackArgumentsLocked(scope, handle, true, memory.UndefinedValue(), memory.NumberValue(timestamp)); err != nil {
		return fmt.Errorf("nativeengine: invoke animation callback %d in task %d: %w", handle, task.TaskID, err)
	}
	return nil
}

func (realm *Realm) Profile() RealmProfile {
	if realm == nil {
		return RealmProfile{Closed: true}
	}
	return realm.profile()
}

func (realm *Realm) profile() RealmProfile {
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	return realm.profileLocked()
}

func (realm *Realm) profileLocked() RealmProfile {
	return RealmProfile{
		ModuleCompilations:    realm.moduleCompilations,
		Evaluations:           realm.evaluations,
		Checkpoints:           realm.checkpoints,
		SourceBytes:           realm.sourceBytes,
		PersistentRegion:      realm.persistentRegion,
		ActiveTask:            realm.activeTask,
		Closed:                realm.closed,
		CheckpointCollections: realm.checkpointCollections,
	}
}

func (realm *Realm) Close() error {
	if realm == nil {
		return nil
	}
	realm.mutex.Lock()
	if realm.closed {
		realm.mutex.Unlock()
		return nil
	}
	realm.closed = true
	profile := realm.profileLocked()
	runtimeRealm := realm.runtime
	persistentRegion := realm.persistentRegion
	activeTask := realm.activeTask
	realm.persistent = nil
	realm.persistentRegion = 0
	realm.persistentWrapperCache = memory.Ref{}
	realm.persistentFacadeCache = memory.Ref{}
	realm.persistentCollectionCache = memory.Ref{}
	realm.persistentCallbackCache = memory.Ref{}
	realm.persistentObserverCache = memory.Ref{}
	realm.persistentModuleCache = memory.Ref{}
	realm.timerCallbacks = nil
	realm.intervalCallbacks = nil
	realm.intervalTimers = nil
	realm.callbackIntervals = nil
	realm.intervalDelays = nil
	realm.animationCallbacks = nil
	realm.mutationObservers = nil
	realm.collectedWrappers = nil
	realm.listeners = nil
	realm.listenerTargets = nil
	realm.activeEvent = nil
	realm.modules = nil
	realm.moduleResolutions = nil
	realm.clearActiveLocked()
	realm.mutex.Unlock()

	if activeTask != 0 {
		realm.interpreter.DiscardJobs(activeTask)
	}
	var result error
	if runtimeRealm != nil && persistentRegion != 0 {
		result = runtimeRealm.Store().DestroyRegion(runtimeRealm.Owner(), persistentRegion)
	}
	if realm.engine != nil {
		realm.engine.forget(realm, profile)
	}
	return result
}

func (realm *Realm) beginTaskLocked(task *browserruntime.TaskContext) (*browserruntime.TaskContext, error) {
	if task == nil || task.Realm == nil {
		return nil, ErrNativeTaskHost
	}
	if realm.runtime == nil {
		realm.runtime = task.Realm
	} else if realm.runtime != task.Realm {
		return nil, ErrRuntimeRealmChange
	}
	if realm.activeTask != 0 {
		if realm.activeTask != task.TaskID {
			return nil, fmt.Errorf("%w: active task %d, new task %d", ErrCheckpointRequired, realm.activeTask, task.TaskID)
		}
		return realm.activeScopeLocked(task)
	}

	if realm.persistent == nil {
		intrinsics, err := realm.interpreter.Bootstrap(task)
		if err != nil {
			return nil, err
		}
		realm.active = intrinsics
		realm.activeRegion = task.MemoryRegion
		realm.activeTask = task.TaskID
		if err := realm.prepareBrowserBindingsLocked(task); err != nil {
			return nil, err
		}
		return task, nil
	}

	scope, err := task.WithBorrowedRealmMemoryRegion(realm.persistentRegion, realm.persistent)
	if err != nil {
		return nil, err
	}
	realm.active = realm.persistent
	realm.activeRegion = realm.persistentRegion
	realm.activeTask = task.TaskID
	if err := realm.prepareBrowserBindingsLocked(scope); err != nil {
		return nil, err
	}
	return scope, nil
}

func (realm *Realm) persistLocked(task *browserruntime.TaskContext) (result error) {
	defer realm.clearActiveLocked()
	if realm.active == nil || realm.runtime == nil {
		return nil
	}
	if realm.persistentRegion != 0 && realm.activeRegion == realm.persistentRegion {
		_, err := task.Realm.Store().CollectRegion(realm.runtime.Owner(), realm.persistentRegion, realm.active.Roots()...)
		if err != nil {
			return fmt.Errorf("nativeengine: collect persistent realm region R%d: %w", realm.persistentRegion, err)
		}
		realm.persistent = realm.active
		return nil
	}
	roots, err := task.Realm.Store().Copy(task.Owner, realm.runtime.Owner(), realm.active.Roots()...)
	if err != nil {
		return err
	}
	newIntrinsics, err := browserruntime.RestoreIntrinsics(roots)
	if err != nil {
		_ = task.Realm.Store().DestroyRegion(realm.runtime.Owner(), roots[0].Region)
		return err
	}
	newRegion := roots[0].Region
	newWrapperCache, err := persistentBindingRef(task.Realm.Store(), realm.runtime.Owner(), newRegion, newIntrinsics.Global, bindingWrapperCache)
	if err != nil {
		_ = task.Realm.Store().DestroyRegion(realm.runtime.Owner(), newRegion)
		return err
	}
	newFacadeCache, err := persistentBindingRef(task.Realm.Store(), realm.runtime.Owner(), newRegion, newIntrinsics.Global, bindingFacadeCache)
	if err != nil {
		_ = task.Realm.Store().DestroyRegion(realm.runtime.Owner(), newRegion)
		return err
	}
	newCollectionCache, err := persistentBindingRef(task.Realm.Store(), realm.runtime.Owner(), newRegion, newIntrinsics.Global, bindingCollectionCache)
	if err != nil {
		_ = task.Realm.Store().DestroyRegion(realm.runtime.Owner(), newRegion)
		return err
	}
	newCallbackCache, err := persistentBindingRef(task.Realm.Store(), realm.runtime.Owner(), newRegion, newIntrinsics.Global, bindingCallbackCache)
	if err != nil {
		_ = task.Realm.Store().DestroyRegion(realm.runtime.Owner(), newRegion)
		return err
	}
	newObserverCache, err := persistentBindingRef(task.Realm.Store(), realm.runtime.Owner(), newRegion, newIntrinsics.Global, bindingObserverCache)
	if err != nil {
		_ = task.Realm.Store().DestroyRegion(realm.runtime.Owner(), newRegion)
		return err
	}
	newModuleCache, err := persistentBindingRef(task.Realm.Store(), realm.runtime.Owner(), newRegion, newIntrinsics.Global, bindingModuleCache)
	if err != nil {
		_ = task.Realm.Store().DestroyRegion(realm.runtime.Owner(), newRegion)
		return err
	}
	if realm.persistentRegion != 0 {
		if err := task.Realm.Store().DestroyRegion(realm.runtime.Owner(), realm.persistentRegion); err != nil {
			_ = task.Realm.Store().DestroyRegion(realm.runtime.Owner(), newRegion)
			return err
		}
	}
	realm.persistent = newIntrinsics
	realm.persistentRegion = newRegion
	realm.persistentWrapperCache = newWrapperCache
	realm.persistentFacadeCache = newFacadeCache
	realm.persistentCollectionCache = newCollectionCache
	realm.persistentCallbackCache = newCallbackCache
	realm.persistentObserverCache = newObserverCache
	realm.persistentModuleCache = newModuleCache
	return nil
}

func (realm *Realm) activeScopeLocked(task *browserruntime.TaskContext) (*browserruntime.TaskContext, error) {
	if realm.persistentRegion != 0 && realm.activeRegion == realm.persistentRegion {
		return task.WithBorrowedRealmMemoryRegion(realm.activeRegion, realm.active)
	}
	return task.WithMemoryRegion(realm.activeRegion, realm.active)
}

func persistentBindingRef(store *memory.Store, owner ownership.OwnerID, region memory.RegionID, global memory.Ref, name string) (memory.Ref, error) {
	nameRef, err := store.AllocString(owner, region, name)
	if err != nil {
		return memory.Ref{}, err
	}
	value, found, resolveErr := store.ResolveBinding(owner, global, nameRef)
	freeErr := store.Free(owner, nameRef)
	if resolveErr != nil {
		return memory.Ref{}, errors.Join(resolveErr, freeErr)
	}
	if freeErr != nil {
		return memory.Ref{}, freeErr
	}
	if !found || !value.IsRef() {
		return memory.Ref{}, fmt.Errorf("nativeengine: missing retained browser binding %q", name)
	}
	return value.Ref(), nil
}

func (realm *Realm) clearActiveLocked() {
	realm.active = nil
	realm.activeRegion = 0
	realm.activeTask = 0
	realm.bindings = nil
}

func runtimeTask(host browser.Host) (*browserruntime.TaskContext, error) {
	native, ok := host.(browser.NativeTaskHost)
	if !ok || native.RuntimeTaskContext() == nil {
		return nil, ErrNativeTaskHost
	}
	return native.RuntimeTaskContext(), nil
}

func evaluationError(url string, err error) error {
	if err == nil {
		return nil
	}
	if url == "" {
		url = "<script>"
	}
	return fmt.Errorf("nativeengine: evaluate %s: %w", url, err)
}

var _ browser.Engine = (*Engine)(nil)
var _ browser.JSRealm = (*Realm)(nil)
var _ browser.JSModuleRealm = (*Realm)(nil)
