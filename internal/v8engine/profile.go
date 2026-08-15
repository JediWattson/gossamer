package v8engine

import (
	"errors"
	"time"
)

const DefaultSamplingInterval = 64 * 1024

type Config struct {
	ICUDataPath      string
	SamplingInterval uint64
}

type HeapStatistics struct {
	TotalHeapSize         uint64 `json:"totalHeapSize"`
	TotalHeapExecutable   uint64 `json:"totalHeapExecutable"`
	TotalPhysicalSize     uint64 `json:"totalPhysicalSize"`
	TotalAvailableSize    uint64 `json:"totalAvailableSize"`
	UsedHeapSize          uint64 `json:"usedHeapSize"`
	HeapSizeLimit         uint64 `json:"heapSizeLimit"`
	MallocedMemory        uint64 `json:"mallocedMemory"`
	ExternalMemory        uint64 `json:"externalMemory"`
	PeakMallocedMemory    uint64 `json:"peakMallocedMemory"`
	NativeContexts        uint64 `json:"nativeContexts"`
	DetachedContexts      uint64 `json:"detachedContexts"`
	GlobalHandlesSize     uint64 `json:"globalHandlesSize"`
	UsedGlobalHandlesSize uint64 `json:"usedGlobalHandlesSize"`
	TotalAllocatedBytes   uint64 `json:"totalAllocatedBytes"`
}

type SamplingProfile struct {
	Samples      uint64 `json:"samples"`
	LiveSamples  uint64 `json:"liveSamples"`
	SampledBytes uint64 `json:"sampledBytes"`
	LiveBytes    uint64 `json:"liveBytes"`
}

type RealmProfile struct {
	Heap                 HeapStatistics  `json:"heap"`
	Sampling             SamplingProfile `json:"sampling"`
	Evaluations          uint64          `json:"evaluations"`
	EvaluationTime       time.Duration   `json:"evaluationNanos"`
	MicrotaskCheckpoints uint64          `json:"microtaskCheckpoints"`
	ForegroundTasks      uint64          `json:"foregroundTasks"`
	GCPrologues          uint64          `json:"gcPrologues"`
	GCEpilogues          uint64          `json:"gcEpilogues"`
	MinorGCs             uint64          `json:"minorGCs"`
	MajorGCs             uint64          `json:"majorGCs"`
	GCTime               time.Duration   `json:"gcNanos"`
	WrappersCreated      uint64          `json:"wrappersCreated"`
	WrapperCacheHits     uint64          `json:"wrapperCacheHits"`
	WrappersCollected    uint64          `json:"wrappersCollected"`
	LiveWrappers         uint64          `json:"liveWrappers"`
	CallbacksCreated     uint64          `json:"callbacksCreated"`
	CallbacksInvoked     uint64          `json:"callbacksInvoked"`
	LiveCallbacks        uint64          `json:"liveCallbacks"`
	EventListeners       uint64          `json:"eventListeners"`
	EventsDispatched     uint64          `json:"eventsDispatched"`
}

type EngineProfile struct {
	Version       string       `json:"version"`
	RealmsCreated uint64       `json:"realmsCreated"`
	RealmsClosed  uint64       `json:"realmsClosed"`
	ClosedRealms  RealmProfile `json:"closedRealms"`
}

var (
	ErrEngineClosed    = errors.New("v8engine: engine is closed")
	ErrRealmClosed     = errors.New("v8engine: realm is closed")
	ErrICUDataRequired = errors.New("v8engine: ICU data path is required")
)

func addRealmProfile(total *RealmProfile, profile RealmProfile) {
	total.Heap = profile.Heap
	total.Sampling.Samples += profile.Sampling.Samples
	total.Sampling.LiveSamples += profile.Sampling.LiveSamples
	total.Sampling.SampledBytes += profile.Sampling.SampledBytes
	total.Sampling.LiveBytes += profile.Sampling.LiveBytes
	total.Evaluations += profile.Evaluations
	total.EvaluationTime += profile.EvaluationTime
	total.MicrotaskCheckpoints += profile.MicrotaskCheckpoints
	total.ForegroundTasks += profile.ForegroundTasks
	total.GCPrologues += profile.GCPrologues
	total.GCEpilogues += profile.GCEpilogues
	total.MinorGCs += profile.MinorGCs
	total.MajorGCs += profile.MajorGCs
	total.GCTime += profile.GCTime
	total.WrappersCreated += profile.WrappersCreated
	total.WrapperCacheHits += profile.WrapperCacheHits
	total.WrappersCollected += profile.WrappersCollected
	total.LiveWrappers += profile.LiveWrappers
	total.CallbacksCreated += profile.CallbacksCreated
	total.CallbacksInvoked += profile.CallbacksInvoked
	total.LiveCallbacks += profile.LiveCallbacks
	total.EventListeners += profile.EventListeners
	total.EventsDispatched += profile.EventsDispatched
}
