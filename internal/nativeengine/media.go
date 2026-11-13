package nativeengine

import (
	"fmt"
	"math"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeMediaAudioConstructor uint64 = 30_000 + iota
	nativeMediaPlay
	nativeMediaPause
	nativeMediaLoad
	nativeMediaCanPlayType
	nativeMediaCurrentSrcGet
	nativeMediaPausedGet
	nativeMediaEndedGet
	nativeMediaMutedGet
	nativeMediaMutedSet
	nativeMediaVolumeGet
	nativeMediaVolumeSet
	nativeMediaCurrentTimeGet
	nativeMediaCurrentTimeSet
	nativeMediaDurationGet
	nativeMediaReadyStateGet
	nativeMediaNetworkStateGet
	nativeMediaPlaybackRateGet
	nativeMediaPlaybackRateSet
	nativeMediaLoopGet
	nativeMediaLoopSet
	nativeMediaAutoplayGet
	nativeMediaAutoplaySet
	nativeMediaControlsGet
	nativeMediaControlsSet
)

type mediaElementState struct {
	paused       bool
	mutedInit    bool
	muted        bool
	volume       float64
	currentTime  float64
	playbackRate float64
}

func (realm *Realm) mediaStateFor(handle browser.NodeHandle) *mediaElementState {
	if realm.mediaStates == nil {
		realm.mediaStates = make(map[browser.NodeHandle]*mediaElementState)
	}
	state, ok := realm.mediaStates[handle]
	if !ok {
		state = &mediaElementState{
			paused:       true,
			volume:       1,
			playbackRate: 1,
		}
		realm.mediaStates[handle] = state
	}
	return state
}

func (realm *Realm) mediaNativeRegistrations() []nativeRegistration {
	return []nativeRegistration{
		{nativeMediaAudioConstructor, realm.audioConstructor},
		{nativeMediaPlay, realm.mediaPlay},
		{nativeMediaPause, realm.mediaPause},
		{nativeMediaLoad, realm.mediaLoad},
		{nativeMediaCanPlayType, realm.mediaCanPlayType},
		{nativeMediaCurrentSrcGet, realm.mediaCurrentSrcGet},
		{nativeMediaPausedGet, realm.mediaPausedGet},
		{nativeMediaEndedGet, realm.mediaEndedGet},
		{nativeMediaMutedGet, realm.mediaMutedGet},
		{nativeMediaMutedSet, realm.mediaMutedSet},
		{nativeMediaVolumeGet, realm.mediaVolumeGet},
		{nativeMediaVolumeSet, realm.mediaVolumeSet},
		{nativeMediaCurrentTimeGet, realm.mediaCurrentTimeGet},
		{nativeMediaCurrentTimeSet, realm.mediaCurrentTimeSet},
		{nativeMediaDurationGet, realm.mediaDurationGet},
		{nativeMediaReadyStateGet, realm.mediaZeroGet},
		{nativeMediaNetworkStateGet, realm.mediaZeroGet},
		{nativeMediaPlaybackRateGet, realm.mediaPlaybackRateGet},
		{nativeMediaPlaybackRateSet, realm.mediaPlaybackRateSet},
		{nativeMediaLoopGet, realm.elementBoolAttrGet("loop")},
		{nativeMediaLoopSet, realm.elementBoolAttrSet("loop")},
		{nativeMediaAutoplayGet, realm.elementBoolAttrGet("autoplay")},
		{nativeMediaAutoplaySet, realm.elementBoolAttrSet("autoplay")},
		{nativeMediaControlsGet, realm.elementBoolAttrGet("controls")},
		{nativeMediaControlsSet, realm.elementBoolAttrSet("controls")},
	}
}

func (realm *Realm) installMediaPrototypeProperties(context *browserruntime.TaskContext) error {
	methods := []struct {
		name  string
		arity uint32
		id    uint64
	}{
		{"play", 0, nativeMediaPlay},
		{"pause", 0, nativeMediaPause},
		{"load", 0, nativeMediaLoad},
		{"canPlayType", 1, nativeMediaCanPlayType},
	}
	for _, method := range methods {
		name, err := newString(context, method.name)
		if err != nil {
			return err
		}
		fn, err := context.NewNativeFunction(name, memory.RefValue(realm.active.Global), method.arity, method.id)
		if err != nil {
			return err
		}
		if err := defineData(context, realm.bindings.htmlMediaElementPrototype, method.name, memory.RefValue(fn), true, false, true); err != nil {
			return err
		}
	}

	accessors := []struct {
		name   string
		getter uint64
		setter uint64
	}{
		{"src", nativeScriptSourceGet, nativeScriptSourceSet},
		{"currentSrc", nativeMediaCurrentSrcGet, 0},
		{"paused", nativeMediaPausedGet, 0},
		{"ended", nativeMediaEndedGet, 0},
		{"muted", nativeMediaMutedGet, nativeMediaMutedSet},
		{"volume", nativeMediaVolumeGet, nativeMediaVolumeSet},
		{"currentTime", nativeMediaCurrentTimeGet, nativeMediaCurrentTimeSet},
		{"duration", nativeMediaDurationGet, 0},
		{"readyState", nativeMediaReadyStateGet, 0},
		{"networkState", nativeMediaNetworkStateGet, 0},
		{"playbackRate", nativeMediaPlaybackRateGet, nativeMediaPlaybackRateSet},
		{"loop", nativeMediaLoopGet, nativeMediaLoopSet},
		{"autoplay", nativeMediaAutoplayGet, nativeMediaAutoplaySet},
		{"controls", nativeMediaControlsGet, nativeMediaControlsSet},
	}
	for _, accessor := range accessors {
		getter, err := realm.newAccessorFunction(context, "get "+accessor.name, accessor.getter, 0)
		if err != nil {
			return err
		}
		setter := memory.UndefinedValue()
		if accessor.setter != 0 {
			ref, setterErr := realm.newAccessorFunction(context, "set "+accessor.name, accessor.setter, 1)
			if setterErr != nil {
				return setterErr
			}
			setter = memory.RefValue(ref)
		}
		if err := defineAccessor(context, realm.bindings.htmlMediaElementPrototype, accessor.name, memory.RefValue(getter), setter); err != nil {
			return err
		}
	}
	return nil
}

func (realm *Realm) newAudioConstructor(context *browserruntime.TaskContext) (memory.Ref, error) {
	name, err := newString(context, "Audio")
	if err != nil {
		return memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(name, memory.RefValue(realm.active.Global), 1, nativeMediaAudioConstructor)
	if err != nil {
		return memory.Ref{}, err
	}
	prototypeName, err := context.NewString("prototype")
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetProperty(constructor, prototypeName, memory.RefValue(realm.bindings.htmlMediaElementPrototype)); err != nil {
		return memory.Ref{}, err
	}
	return constructor, nil
}

func (realm *Realm) audioConstructor(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.host.CreateElement("audio")
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	_ = realm.host.SetAttribute(handle, "preload", "auto")
	if srcArg := argument(arguments, 0); srcArg.Kind() != memory.ValueUndefined {
		src, strErr := valueString(context, srcArg)
		if strErr != nil {
			return memory.Value{}, strErr
		}
		if err := realm.host.SetAttribute(handle, "src", src); err != nil {
			return memory.Value{}, realm.throwDOMException(context, err)
		}
	}
	return realm.wrappedNodeValue(context, handle)
}

func (realm *Realm) unwrapMediaHandle(context *browserruntime.TaskContext, this memory.Value) (browser.NodeHandle, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return browser.NodeHandle{}, err
	}
	meta, err := realm.nodeMetadata(context, this)
	if err != nil {
		return browser.NodeHandle{}, realm.throwDOMException(context, err)
	}
	if meta.Type != browser.DOMElementNode || (!strings.EqualFold(meta.LocalName, "audio") && !strings.EqualFold(meta.LocalName, "video")) {
		return browser.NodeHandle{}, fmt.Errorf("%w: receiver is not an HTMLMediaElement", browserruntime.ErrOperandType)
	}
	return handle, nil
}

func (realm *Realm) mediaPlay(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	state := realm.mediaStateFor(handle)
	state.paused = false
	promise, err := context.NewPromise()
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.ResolvePromise(promise, memory.UndefinedValue()); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(promise), nil
}

func (realm *Realm) mediaPause(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	state := realm.mediaStateFor(handle)
	state.paused = true
	return memory.UndefinedValue(), nil
}

func (realm *Realm) mediaLoad(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	state := realm.mediaStateFor(handle)
	state.paused = true
	state.currentTime = 0
	return memory.UndefinedValue(), nil
}

func (realm *Realm) mediaCanPlayType(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapMediaHandle(context, this); err != nil {
		return memory.Value{}, err
	}
	return newString(context, "")
}

func (realm *Realm) mediaCurrentSrcGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	src, found, err := realm.host.GetAttribute(handle, "src")
	if err != nil || !found {
		return newString(context, "")
	}
	return newString(context, src)
}

func (realm *Realm) mediaPausedGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.BoolValue(realm.mediaStateFor(handle).paused), nil
}

func (realm *Realm) mediaEndedGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapMediaHandle(context, this); err != nil {
		return memory.Value{}, err
	}
	return memory.BoolValue(false), nil
}

func (realm *Realm) mediaMutedGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	state := realm.mediaStateFor(handle)
	if state.mutedInit {
		return memory.BoolValue(state.muted), nil
	}
	_, has, err := realm.host.GetAttribute(handle, "muted")
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	return memory.BoolValue(has), nil
}

func (realm *Realm) mediaMutedSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	state := realm.mediaStateFor(handle)
	state.mutedInit = true
	state.muted = canvasBool(context, argument(arguments, 0))
	return memory.UndefinedValue(), nil
}

func (realm *Realm) mediaVolumeGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(realm.mediaStateFor(handle).volume), nil
}

func (realm *Realm) mediaVolumeSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	n, err := canvasNumber(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if !isFiniteFloat(n) || n < 0 || n > 1 {
		return memory.Value{}, realm.throwDOMException(context, dom.NewException(dom.IndexSizeError, nil, "volume out of range [0, 1]"))
	}
	state := realm.mediaStateFor(handle)
	state.volume = n
	return memory.UndefinedValue(), nil
}

func (realm *Realm) mediaCurrentTimeGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(realm.mediaStateFor(handle).currentTime), nil
}

func (realm *Realm) mediaCurrentTimeSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	n, err := canvasNumber(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if isFiniteFloat(n) && n >= 0 {
		realm.mediaStateFor(handle).currentTime = n
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) mediaDurationGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapMediaHandle(context, this); err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(math.NaN()), nil
}

func (realm *Realm) mediaZeroGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapMediaHandle(context, this); err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(0), nil
}

func (realm *Realm) mediaPlaybackRateGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(realm.mediaStateFor(handle).playbackRate), nil
}

func (realm *Realm) mediaPlaybackRateSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapMediaHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	n, err := canvasNumber(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if isFiniteFloat(n) {
		realm.mediaStateFor(handle).playbackRate = n
	}
	return memory.UndefinedValue(), nil
}
