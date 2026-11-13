package window

import "time"

// FrameTiming records one native presentation without exposing Page, DOM, or
// renderer-owned pointers to profiling consumers. Durations measure only work
// performed from native-event admission through presentation; SincePrevious
// also captures the observed presentation cadence. Tasks includes synchronous
// event routing and page work before rasterization.
type FrameTiming struct {
	Sequence                  uint64        `json:"sequence"`
	PresentedAt               time.Time     `json:"presented_at"`
	SincePrevious             time.Duration `json:"since_previous_ns"`
	Tasks                     time.Duration `json:"tasks_ns"`
	Raster                    time.Duration `json:"raster_ns"`
	Composite                 time.Duration `json:"composite_ns"`
	Present                   time.Duration `json:"present_ns"`
	Total                     time.Duration `json:"total_ns"`
	PageChanged               bool          `json:"page_changed"`
	ShellChanged              bool          `json:"shell_changed"`
	ScrollFrame               bool          `json:"scroll_frame"`
	IncrementalRaster         bool          `json:"incremental_raster"`
	DamagePixels              uint64        `json:"damage_pixels"`
	DamageRects               uint64        `json:"damage_rects"`
	PlannedDamagePixels       uint64        `json:"planned_damage_pixels"`
	PlannedDamageRects        uint64        `json:"planned_damage_rects"`
	PlannedScrollClipArea     uint64        `json:"planned_scroll_clip_area"`
	PlannedRawDamageRects     uint64        `json:"planned_raw_damage_rects"`
	PlannedExactDamagePixels  uint64        `json:"planned_exact_damage_pixels"`
	PlannedExactDamageRects   uint64        `json:"planned_exact_damage_rects"`
	PlannedDamageSource       string        `json:"planned_damage_source"`
	PlannedSourceDamagePixels uint64        `json:"planned_source_damage_pixels"`
	PlannedSourceDamageRects  uint64        `json:"planned_source_damage_rects"`
	PlannedScrollDeltaX       int64         `json:"planned_scroll_delta_x"`
	PlannedScrollDeltaY       int64         `json:"planned_scroll_delta_y"`
	PlannedScrollCommands     uint64        `json:"planned_scroll_commands"`
	PlannedMovingCommands     uint64        `json:"planned_moving_commands"`
	RetainedUnderlay          bool          `json:"retained_underlay"`
	UnderlayCacheHit          bool          `json:"underlay_cache_hit"`
	UnderlayCacheMiss         string        `json:"underlay_cache_miss"`
	OverlaySuffixCommands     uint64        `json:"overlay_suffix_commands"`
	OverlayRegions            uint64        `json:"overlay_regions"`
	UnderlayCopyPixels        uint64        `json:"underlay_copy_pixels"`
	UnderlayBuild             time.Duration `json:"underlay_build_ns"`
	ScrollCopy                time.Duration `json:"scroll_copy_ns"`
	DamageReplay              time.Duration `json:"damage_replay_ns"`
	UnderlayCopy              time.Duration `json:"underlay_copy_ns"`
	OverlayReplay             time.Duration `json:"overlay_replay_ns"`
	RasterFallback            string        `json:"raster_fallback"`
}

// FrameTimingObserver receives presentations serially on the native session
// thread. Implementations should buffer output so profiling does not become a
// frame-time bottleneck.
type FrameTimingObserver func(FrameTiming) error
