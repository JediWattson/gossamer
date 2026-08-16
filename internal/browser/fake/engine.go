// Package fake provides an engine-neutral test implementation. It proves the
// browser/host contract without embedding a JavaScript engine.
package fake

import (
	"errors"
	"fmt"
	"sync"

	"github.com/JediWattson/gossamer/internal/browser"
)

type Callback func(browser.Host) error

type Engine struct {
	mutex       sync.Mutex
	realms      []*Realm
	initializer func(*Realm) error
	closed      bool
}

func New() *Engine {
	return &Engine{}
}

func NewWithInitializer(initializer func(*Realm) error) *Engine {
	return &Engine{initializer: initializer}
}

func (engine *Engine) NewRealm() (browser.JSRealm, error) {
	if engine == nil {
		return nil, fmt.Errorf("fake engine: nil engine")
	}
	engine.mutex.Lock()
	if engine.closed {
		engine.mutex.Unlock()
		return nil, ErrEngineClosed
	}
	initializer := engine.initializer
	engine.mutex.Unlock()
	realm := &Realm{
		callbacks: make(map[browser.ValueHandle]Callback),
		handlers:  make(map[eventTarget]browser.ValueHandle),
	}
	if initializer != nil {
		if err := initializer(realm); err != nil {
			_ = realm.Close()
			return nil, err
		}
	}
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	if engine.closed {
		_ = realm.Close()
		return nil, ErrEngineClosed
	}
	engine.realms = append(engine.realms, realm)
	return realm, nil
}

func (engine *Engine) LatestRealm() (*Realm, bool) {
	if engine == nil {
		return nil, false
	}
	engine.mutex.Lock()
	defer engine.mutex.Unlock()
	if len(engine.realms) == 0 {
		return nil, false
	}
	return engine.realms[len(engine.realms)-1], true
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
	realms := append([]*Realm(nil), engine.realms...)
	engine.mutex.Unlock()
	var result error
	for _, realm := range realms {
		result = errors.Join(result, realm.Close())
	}
	return result
}

type Realm struct {
	mutex           sync.Mutex
	nextCallback    browser.ValueHandle
	callbacks       map[browser.ValueHandle]Callback
	handlers        map[eventTarget]browser.ValueHandle
	evaluator       CallbackEvaluator
	moduleEvaluator ModuleEvaluator
	closed          bool
}

type CallbackEvaluator func(browser.Host, browser.ScriptSource) error
type ModuleEvaluator func(browser.Host, browser.ModuleGraph) error

type eventTarget struct {
	typeID browser.InputEventType
	target browser.NodeHandle
}

func (realm *Realm) RegisterCallback(callback Callback) (browser.ValueHandle, error) {
	if realm == nil || callback == nil {
		return 0, fmt.Errorf("fake engine: nil realm or callback")
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return 0, ErrRealmClosed
	}
	realm.nextCallback++
	handle := realm.nextCallback
	realm.callbacks[handle] = callback
	return handle, nil
}

func (realm *Realm) Bind(eventType browser.InputEventType, target browser.NodeHandle, callback browser.ValueHandle) error {
	if realm == nil {
		return fmt.Errorf("fake engine: nil realm")
	}
	if eventType == 0 || target.Document == 0 || target.Node == 0 || callback == 0 {
		return fmt.Errorf("fake engine: invalid event binding")
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return ErrRealmClosed
	}
	if realm.callbacks[callback] == nil {
		return fmt.Errorf("%w: %d", ErrUnknownCallback, callback)
	}
	realm.handlers[eventTarget{typeID: eventType, target: target}] = callback
	return nil
}

func (realm *Realm) SetEvaluator(evaluator CallbackEvaluator) error {
	if realm == nil || evaluator == nil {
		return fmt.Errorf("fake engine: nil realm or evaluator")
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return ErrRealmClosed
	}
	realm.evaluator = evaluator
	return nil
}

func (realm *Realm) SetModuleEvaluator(evaluator ModuleEvaluator) error {
	if realm == nil || evaluator == nil {
		return fmt.Errorf("fake engine: nil realm or module evaluator")
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return ErrRealmClosed
	}
	realm.moduleEvaluator = evaluator
	return nil
}

func (realm *Realm) Evaluate(host browser.Host, source browser.ScriptSource) error {
	if host == nil {
		return fmt.Errorf("fake engine: nil host")
	}
	realm.mutex.Lock()
	if realm.closed {
		realm.mutex.Unlock()
		return ErrRealmClosed
	}
	evaluator := realm.evaluator
	realm.mutex.Unlock()
	if evaluator == nil {
		return nil
	}
	return evaluator(host, source)
}

func (realm *Realm) EvaluateModule(host browser.Host, graph browser.ModuleGraph) error {
	if host == nil {
		return fmt.Errorf("fake engine: nil host")
	}
	realm.mutex.Lock()
	if realm.closed {
		realm.mutex.Unlock()
		return ErrRealmClosed
	}
	evaluator := realm.moduleEvaluator
	realm.mutex.Unlock()
	if evaluator == nil {
		return nil
	}
	return evaluator(host, graph)
}

func (realm *Realm) DispatchEvent(host browser.Host, event browser.InputEvent) (browser.EventDispatchResult, error) {
	if host == nil {
		return browser.EventDispatchResult{}, fmt.Errorf("fake engine: nil host")
	}
	realm.mutex.Lock()
	if realm.closed {
		realm.mutex.Unlock()
		return browser.EventDispatchResult{}, ErrRealmClosed
	}
	callback := realm.handlers[eventTarget{typeID: event.Type, target: event.Target}]
	realm.mutex.Unlock()
	if callback == 0 {
		return browser.EventDispatchResult{}, nil
	}
	return browser.EventDispatchResult{}, host.QueueCallback(callback)
}

func (realm *Realm) Invoke(host browser.Host, handle browser.ValueHandle) error {
	if host == nil {
		return fmt.Errorf("fake engine: nil host")
	}
	realm.mutex.Lock()
	if realm.closed {
		realm.mutex.Unlock()
		return ErrRealmClosed
	}
	callback := realm.callbacks[handle]
	realm.mutex.Unlock()
	if callback == nil {
		return fmt.Errorf("%w: %d", ErrUnknownCallback, handle)
	}
	return callback(host)
}

// DrainMicrotasks is intentionally empty: fake callbacks can use
// Host.QueueMicrotask to exercise Gossamer's queue, while a V8 adapter will run
// its isolate microtask checkpoint here.
func (*Realm) DrainMicrotasks(browser.Host) error { return nil }

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
	realm.callbacks = nil
	realm.handlers = nil
	realm.evaluator = nil
	realm.moduleEvaluator = nil
	realm.mutex.Unlock()
	return nil
}

var (
	ErrEngineClosed    = errors.New("fake engine: engine is closed")
	ErrRealmClosed     = errors.New("fake engine: realm is closed")
	ErrUnknownCallback = errors.New("fake engine: unknown callback")
)
