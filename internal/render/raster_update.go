package render

import (
	"image"
	"image/draw"
	"math"
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
)

// RasterFallbackReason explains why a retained framebuffer update used a full
// replay. The empty value means the update was incremental.
type RasterFallbackReason string

const (
	RasterFallbackArtifact        RasterFallbackReason = "artifact"
	RasterFallbackTopology        RasterFallbackReason = "topology"
	RasterFallbackPayload         RasterFallbackReason = "payload"
	RasterFallbackClipMotion      RasterFallbackReason = "clip_motion"
	RasterFallbackFractional      RasterFallbackReason = "fractional"
	RasterFallbackMultipleScroll  RasterFallbackReason = "multiple_scroll"
	RasterFallbackUnsafeGroup     RasterFallbackReason = "unsafe_group"
	RasterFallbackExcessiveDamage RasterFallbackReason = "excessive_damage"
	RasterFallbackInvalidPrevious RasterFallbackReason = "invalid_previous"
	RasterFallbackNoScroll        RasterFallbackReason = "no_scroll"
)

func (reason RasterFallbackReason) String() string { return string(reason) }

// RasterUpdate is one full or retained-framebuffer replay result. Incremental
// updates mutate and return the supplied previous canvas. Full fallbacks return
// a newly allocated canvas and report the full device-pixel area as damage.
type RasterUpdate struct {
	Canvas                    *image.RGBA
	Incremental               bool
	DamagePixels              int
	DamageRects               int
	PlannedDamagePixels       int
	PlannedDamageRects        int
	PlannedScrollClipArea     int
	PlannedRawDamageRects     int
	PlannedExactDamagePixels  int
	PlannedExactDamageRects   int
	PlannedDamageSource       string
	PlannedSourceDamagePixels int
	PlannedSourceDamageRects  int
	PlannedScrollDeltaX       int
	PlannedScrollDeltaY       int
	PlannedScrollCommands     int
	PlannedMovingCommands     int
	RetainedUnderlay          bool
	UnderlayCacheHit          bool
	UnderlayCacheMiss         string
	OverlaySuffixCommands     int
	OverlayRegions            int
	UnderlayCopyPixels        int
	UnderlayBuild             time.Duration
	ScrollCopy                time.Duration
	DamageReplay              time.Duration
	UnderlayCopy              time.Duration
	OverlayReplay             time.Duration
	Fallback                  RasterFallbackReason
}

const (
	maxRasterDamageRects = 8
	maxRasterDamageRatio = 0.60
	maxRasterWorkers     = 4
	maxOverlayRegions    = 64
	textDamagePadding    = 2
)

type overlayReplayJob struct {
	displayList DisplayList
	fonts       *fontBook
	scale       float64
	viewport    Viewport
	target      *image.RGBA
	rectangle   image.Rectangle
	groupBounds map[int]image.Rectangle
	paintStart  int
	paintEnd    int
	paintCache  *rasterOverlayPaintCache
}

type overlayReplayResult struct {
	err error
}

type scrollDamagePlan struct {
	clip               image.Rectangle
	delta              image.Point
	damage             []image.Rectangle
	damagePixels       int
	rawDamageRects     int
	exactDamagePixels  int
	exactDamageRects   int
	damageSource       string
	sourceDamagePixels int
	sourceDamageRects  int
	scrollCommands     int
	movingCommands     int
	overlayStart       int
}

type retainedOverlayIdentity struct {
	node       *dom.Node
	command    Command
	image      image.Image
	hasCommand bool
}

type projectedCommandMotion struct {
	kind              CommandKind
	delta             image.Point
	painted           bool
	oldBounds         image.Rectangle
	newBounds         image.Rectangle
	stationaryClips   []image.Rectangle
	commandClip       projectedRectangularClip
	hasCommandClip    bool
	stationaryRounded bool
	roundedFill       bool
	uniformFill       bool
	fillContextSafe   bool
	fillBlocker       string
	uniformOldBounds  image.Rectangle
	uniformNewBounds  image.Rectangle
	requiresReplay    bool
	transformBegin    bool
	oldTransform      AffineTransform
	newTransform      AffineTransform
	roundedGroup      bool
}

// projectedRectangularClip records both sides of a command-local hard clip.
// The retained framebuffer contains pixels masked by old; after the candidate
// scroll copy those pixels occupy old.Add(scroll), while the target frame is
// masked by new. Keeping both rectangles lets the planner repair their exact
// symmetric difference when layout changes the scrollport dimensions.
type projectedRectangularClip struct {
	old image.Rectangle
	new image.Rectangle
}

// UpdateAtScale attempts to update previous by shifting one rectangular,
// integer-device-pixel scroll layer and replaying the changing prefix only in
// exact damage regions. A retained stationary suffix is then replayed over the
// repaired underlay. All ambiguous cases use a full replay.
func (rasterizer *FrameRasterizer) UpdateAtScale(previous *image.RGBA, oldFrame, newFrame *Frame, scale float64) (RasterUpdate, error) {
	if rasterizer == nil {
		return RasterUpdate{}, errorf("render: nil frame rasterizer")
	}
	if newFrame == nil {
		return RasterUpdate{}, errorf("render: nil frame")
	}
	viewport, err := scaledViewport(newFrame.DisplayList.Viewport, scale)
	if err != nil {
		return RasterUpdate{}, err
	}

	rasterizer.mutex.Lock()
	defer rasterizer.mutex.Unlock()
	if rasterizer.closed {
		return RasterUpdate{}, errorf("render: frame rasterizer is closed")
	}
	if err := rasterizer.prepareFontsLocked(newFrame.Fonts); err != nil {
		return RasterUpdate{}, err
	}

	plan, fallback := planScrollDamage(previous, oldFrame, newFrame, scale, viewport)
	if fallback != "" {
		if plan.overlayStart > 0 {
			if identity := overlayIdentityAt(newFrame.DisplayList.Commands, plan.overlayStart); identity.node != nil {
				rasterizer.scrollOverlay = identity
			}
		}
		update, err := rasterizer.fullRasterUpdateLocked(newFrame, scale, viewport, fallback)
		update.PlannedDamagePixels = plan.damagePixels
		update.PlannedDamageRects = len(plan.damage)
		update.PlannedScrollClipArea = rectangleArea(plan.clip)
		update.PlannedRawDamageRects = plan.rawDamageRects
		update.PlannedExactDamagePixels = plan.exactDamagePixels
		update.PlannedExactDamageRects = plan.exactDamageRects
		update.PlannedDamageSource = plan.damageSource
		update.PlannedSourceDamagePixels = plan.sourceDamagePixels
		update.PlannedSourceDamageRects = plan.sourceDamageRects
		update.PlannedScrollDeltaX = plan.delta.X
		update.PlannedScrollDeltaY = plan.delta.Y
		update.PlannedScrollCommands = plan.scrollCommands
		update.PlannedMovingCommands = plan.movingCommands
		return update, err
	}

	deviceBounds := image.Rect(0, 0, viewport.Width, viewport.Height)
	retainedUnderlay := false
	underlayCacheHit := false
	underlayCacheMiss := ""
	overlaySuffixCommands := 0
	overlayRegions := 0
	underlayCopyPixels := 0
	underlayBuild := time.Duration(0)
	scrollCopy := time.Duration(0)
	damageReplay := time.Duration(0)
	underlayCopy := time.Duration(0)
	overlayReplay := time.Duration(0)
	if plan.overlayStart > 0 {
		retainedUnderlay = true
		underlayCacheMiss = rasterizer.retainedUnderlayMissLocked(oldFrame, scale, viewport, plan.overlayStart)
		underlayCacheHit = underlayCacheMiss == ""
		overlaySuffixCommands = len(newFrame.DisplayList.Commands) - plan.overlayStart
		underlayCopyPixels = rectangleArea(plan.clip)
		phaseStarted := time.Now()
		if err := rasterizer.ensureRetainedUnderlayLocked(oldFrame, scale, viewport, plan.overlayStart); err != nil {
			return RasterUpdate{}, err
		}
		if !underlayCacheHit {
			underlayBuild = time.Since(phaseStarted)
		}
		phaseStarted = time.Now()
		copyScrollRectangle(rasterizer.scrollBase, plan.clip, plan.delta)
		scrollCopy = time.Since(phaseStarted)
		groupBounds := rasterGroupBoundsAtScaleRange(newFrame.DisplayList.Commands, deviceBounds, scale, 0, plan.overlayStart)
		phaseStarted = time.Now()
		if err := rasterizer.replayDamageRegionsRangeLocked(newFrame.DisplayList, scale, viewport, rasterizer.scrollBase, plan.damage, groupBounds, 0, plan.overlayStart); err != nil {
			rasterizer.clearRetainedUnderlayLocked()
			return RasterUpdate{}, err
		}
		damageReplay = time.Since(phaseStarted)
		phaseStarted = time.Now()
		draw.Draw(previous, plan.clip, rasterizer.scrollBase, plan.clip.Min, draw.Src)
		underlayCopy = time.Since(phaseStarted)
		overlayGroups, transformOutputs := rasterGroupBoundsAndTransformOutputsAtScaleRange(
			newFrame.DisplayList.Commands, deviceBounds, scale, plan.overlayStart, len(newFrame.DisplayList.Commands),
		)
		regions := overlayPaintRegions(newFrame.DisplayList.Commands, rasterizer.fonts, scale, plan.clip, overlayGroups, transformOutputs, plan.overlayStart, len(newFrame.DisplayList.Commands))
		overlayRegions = len(regions)
		phaseStarted = time.Now()
		rasterizer.overlayPaint.prepare(newFrame.paintArtifact)
		if err := rasterizer.replayOverlayRegionsLocked(
			newFrame.DisplayList, scale, viewport, previous, overlayGroups,
			plan.overlayStart, len(newFrame.DisplayList.Commands), regions,
		); err != nil {
			rasterizer.clearRetainedUnderlayLocked()
			return RasterUpdate{}, err
		}
		overlayReplay = time.Since(phaseStarted)
		rasterizer.scrollBaseFrame = newFrame
		rasterizer.scrollBaseScale = scale
		rasterizer.scrollBaseStart = plan.overlayStart
		if identity := overlayIdentityAt(newFrame.DisplayList.Commands, plan.overlayStart); identity.node != nil {
			rasterizer.scrollOverlay = identity
		}
	} else {
		phaseStarted := time.Now()
		copyScrollRectangle(previous, plan.clip, plan.delta)
		scrollCopy = time.Since(phaseStarted)
		groupBounds := rasterGroupBoundsAtScale(newFrame.DisplayList.Commands, deviceBounds, scale)
		phaseStarted = time.Now()
		if err := rasterizer.replayDamageRegionsLocked(newFrame.DisplayList, scale, viewport, previous, plan.damage, groupBounds); err != nil {
			return RasterUpdate{}, err
		}
		damageReplay = time.Since(phaseStarted)
		rasterizer.clearRetainedUnderlayLocked()
		rasterizer.scrollOverlay = retainedOverlayIdentity{}
	}
	return RasterUpdate{
		Canvas: previous, Incremental: true,
		DamagePixels: plan.damagePixels, DamageRects: len(plan.damage),
		PlannedDamagePixels: plan.damagePixels, PlannedDamageRects: len(plan.damage),
		PlannedScrollClipArea:     rectangleArea(plan.clip),
		PlannedRawDamageRects:     plan.rawDamageRects,
		PlannedScrollDeltaX:       plan.delta.X,
		PlannedScrollDeltaY:       plan.delta.Y,
		PlannedScrollCommands:     plan.scrollCommands,
		PlannedMovingCommands:     plan.movingCommands,
		PlannedDamageSource:       plan.damageSource,
		PlannedSourceDamagePixels: plan.sourceDamagePixels,
		PlannedSourceDamageRects:  plan.sourceDamageRects,
		RetainedUnderlay:          retainedUnderlay,
		UnderlayCacheHit:          underlayCacheHit,
		UnderlayCacheMiss:         underlayCacheMiss,
		OverlaySuffixCommands:     overlaySuffixCommands,
		OverlayRegions:            overlayRegions,
		UnderlayCopyPixels:        underlayCopyPixels,
		UnderlayBuild:             underlayBuild,
		ScrollCopy:                scrollCopy,
		DamageReplay:              damageReplay,
		UnderlayCopy:              underlayCopy,
		OverlayReplay:             overlayReplay,
	}, nil
}

// replayDamageRegions paints disjoint retained-framebuffer repairs in parallel.
// Frames and their image payloads are immutable, group bounds are read-only,
// each worker owns its raster layers and clip masks, and fontBook serializes
// mutable face caches. No worker writes outside its damage subimage.
func (rasterizer *FrameRasterizer) replayDamageRegionsLocked(displayList DisplayList, scale float64, viewport Viewport, target *image.RGBA, damage []image.Rectangle, groupBounds map[int]image.Rectangle) error {
	return rasterizer.replayDamageRegionsRangeLocked(displayList, scale, viewport, target, damage, groupBounds, 0, len(displayList.Commands))
}

func (rasterizer *FrameRasterizer) replayDamageRegionsRangeLocked(displayList DisplayList, scale float64, viewport Viewport, target *image.RGBA, damage []image.Rectangle, groupBounds map[int]image.Rectangle, paintStart, paintEnd int) error {
	paint := func(fonts *fontBook, rectangle image.Rectangle) error {
		draw.Draw(target, rectangle, image.Transparent, image.Point{}, draw.Src)
		_, err := rasterizeAtScaleIntoRangeWithGroups(displayList, fonts, scale, viewport, target, rectangle, groupBounds, paintStart, paintEnd)
		return err
	}
	if len(damage) < 2 || runtime.GOMAXPROCS(0) < 2 {
		for _, rectangle := range damage {
			if err := paint(rasterizer.fonts, rectangle); err != nil {
				return err
			}
		}
		return nil
	}

	workerCount := min(len(damage), runtime.GOMAXPROCS(0), maxRasterWorkers)

	errors := make([]error, len(damage))
	jobs := make(chan int)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for index := range jobs {
				errors[index] = paint(rasterizer.fonts, damage[index])
			}
		}()
	}
	for index := range damage {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

// replayOverlayRegionsLocked splits a stationary suffix into disjoint painted
// components. Each component preserves the complete suffix order, while top
// and bottom overlays can blend over the changing underlay concurrently.
func (rasterizer *FrameRasterizer) replayOverlayRegionsLocked(displayList DisplayList, scale float64, viewport Viewport, target *image.RGBA, groupBounds map[int]image.Rectangle, paintStart, paintEnd int, regions []image.Rectangle) error {
	paint := func(rectangle image.Rectangle) error {
		_, err := rasterizeAtScaleIntoRangeWithGroupsOverlayCache(
			displayList, rasterizer.fonts, scale, viewport, target, rectangle, groupBounds,
			paintStart, paintEnd, &rasterizer.overlayPaint,
		)
		return err
	}
	if len(regions) < 2 || runtime.GOMAXPROCS(0) < 2 || len(regions) > maxOverlayRegions {
		for _, rectangle := range regions {
			if err := paint(rectangle); err != nil {
				return err
			}
		}
		return nil
	}

	rasterizer.startOverlayWorkersLocked()
	for _, rectangle := range regions {
		rasterizer.overlayJobs <- overlayReplayJob{
			displayList: displayList, fonts: rasterizer.fonts, scale: scale, viewport: viewport,
			target: target, rectangle: rectangle, groupBounds: groupBounds,
			paintStart: paintStart, paintEnd: paintEnd, paintCache: &rasterizer.overlayPaint,
		}
	}
	var firstError error
	for range regions {
		if result := <-rasterizer.overlayResults; result.err != nil && firstError == nil {
			firstError = result.err
		}
	}
	return firstError
}

func (rasterizer *FrameRasterizer) startOverlayWorkersLocked() {
	if rasterizer.overlayJobs != nil {
		return
	}
	rasterizer.overlayJobs = make(chan overlayReplayJob, maxOverlayRegions)
	rasterizer.overlayResults = make(chan overlayReplayResult, maxOverlayRegions)
	workerCount := min(runtime.GOMAXPROCS(0), maxRasterWorkers)
	rasterizer.overlayWorkers.Add(workerCount)
	for range workerCount {
		go func() {
			defer rasterizer.overlayWorkers.Done()
			for job := range rasterizer.overlayJobs {
				_, err := rasterizeAtScaleIntoRangeWithGroupsOverlayCache(
					job.displayList, job.fonts, job.scale, job.viewport, job.target, job.rectangle, job.groupBounds,
					job.paintStart, job.paintEnd, job.paintCache,
				)
				rasterizer.overlayResults <- overlayReplayResult{err: err}
			}
		}()
	}
}

func (rasterizer *FrameRasterizer) closeOverlayWorkersLocked() {
	if rasterizer.overlayJobs == nil {
		return
	}
	close(rasterizer.overlayJobs)
	rasterizer.overlayWorkers.Wait()
	rasterizer.overlayJobs = nil
	rasterizer.overlayResults = nil
}

func overlayPaintRegions(commands []Command, fonts *fontBook, scale float64, clip image.Rectangle, groupBounds, transformOutputs map[int]image.Rectangle, paintStart, paintEnd int) []image.Rectangle {
	candidates := make([]image.Rectangle, 0, paintEnd-paintStart)
	transformDepth := 0
	for index := max(0, paintStart); index < min(len(commands), paintEnd); index++ {
		command := commands[index]
		scaleRasterCommand(&command, scale)
		switch command.Kind {
		case BeginTransformCommand:
			if transformDepth != 0 {
				// Nested outputs are expressed in their parent layer. Replaying
				// one clip remains exact until the planner composes that chain.
				return []image.Rectangle{clip}
			}
			if bounds := transformOutputs[index].Intersect(clip); !bounds.Empty() {
				candidates = append(candidates, bounds)
			}
			transformDepth++
			continue
		case EndTransformCommand:
			if transformDepth > 0 {
				transformDepth--
			}
			continue
		case DrawTextCommand:
			if transformDepth > 0 {
				continue
			}
			// textBounds carries line/advance geometry, so use the exact hinted
			// glyph rectangles that fontBook.draw will composite. Vertical text
			// uses separately rasterized intermediate surfaces and remains a
			// conservative one-region replay for now.
			if ink, exact := fonts.horizontalTextInkBounds(command); exact {
				ink = ink.Intersect(clip)
				if command.HasClip {
					ink = ink.Intersect(rectImageBounds(command.Clip))
				}
				for clipIndex := 0; clipIndex < int(command.RoundedClipCount); clipIndex++ {
					ink = ink.Intersect(rectImageBounds(command.RoundedClips[clipIndex].Rect))
				}
				if !ink.Empty() {
					candidates = append(candidates, ink)
				}
				continue
			}
			return []image.Rectangle{clip}
		case BeginOpacityCommand:
			if transformDepth > 0 {
				continue
			}
			if bounds := groupBounds[index].Intersect(clip); !bounds.Empty() {
				candidates = append(candidates, bounds)
			}
		}
		if transformDepth > 0 {
			continue
		}
		if !commandPaintsPixels(command.Kind) {
			continue
		}
		rectangle := scaledCommandBounds(commands[index], scale, clip)
		if !rectangle.Empty() {
			candidates = append(candidates, rectangle)
		}
	}

	regions := make([]image.Rectangle, 0, len(candidates))
	for _, candidate := range candidates {
		for index := 0; index < len(regions); {
			if !regions[index].Overlaps(candidate) {
				index++
				continue
			}
			candidate = candidate.Union(regions[index])
			regions[index] = regions[len(regions)-1]
			regions = regions[:len(regions)-1]
			index = 0
		}
		regions = append(regions, candidate)
	}
	if len(regions) > maxOverlayRegions {
		// Each region requires one ordered suffix scan. Bound adversarial pages
		// with many disjoint badges/tooltips to one conservative replay rather
		// than multiplying display-list work without limit.
		return []image.Rectangle{clip}
	}
	return regions
}

func (rasterizer *FrameRasterizer) ensureRetainedUnderlayLocked(frame *Frame, scale float64, viewport Viewport, overlayStart int) error {
	bounds := image.Rect(0, 0, viewport.Width, viewport.Height)
	if rasterizer.matchesRetainedUnderlayLocked(frame, scale, viewport, overlayStart) {
		return nil
	}
	canvas, err := rasterizeAtScaleIntoRangeWithGroups(
		frame.DisplayList, rasterizer.fonts, scale, viewport, nil, bounds, nil, 0, overlayStart,
	)
	if err != nil {
		return err
	}
	rasterizer.scrollBase = canvas
	rasterizer.scrollBaseFrame = frame
	rasterizer.scrollBaseScale = scale
	rasterizer.scrollBaseStart = overlayStart
	return nil
}

func (rasterizer *FrameRasterizer) matchesRetainedUnderlayLocked(frame *Frame, scale float64, viewport Viewport, overlayStart int) bool {
	return rasterizer.retainedUnderlayMissLocked(frame, scale, viewport, overlayStart) == ""
}

func (rasterizer *FrameRasterizer) retainedUnderlayMissLocked(frame *Frame, scale float64, viewport Viewport, overlayStart int) string {
	bounds := image.Rect(0, 0, viewport.Width, viewport.Height)
	switch {
	case rasterizer.scrollBase == nil:
		return "empty"
	case rasterizer.scrollBase.Bounds() != bounds:
		return "bounds"
	case !sameRetainedUnderlayFrame(rasterizer.scrollBaseFrame, frame):
		return "frame"
	case rasterizer.scrollBaseScale != scale:
		return "scale"
	case rasterizer.scrollBaseStart != overlayStart:
		if retainedUnderlayBoundariesEquivalent(frame.DisplayList.Commands, rasterizer.scrollBaseStart, overlayStart) {
			return ""
		}
		return "boundary"
	default:
		return ""
	}
}

func sameRetainedUnderlayFrame(left, right *Frame) bool {
	if left == right {
		return left != nil
	}
	if left == nil || right == nil {
		return false
	}
	if left.paintArtifact == 0 || left.paintArtifact != right.paintArtifact {
		return false
	}
	if left.Viewport != right.Viewport || left.DisplayList.Viewport != right.DisplayList.Viewport {
		return false
	}
	return sameCommandSlice(left.DisplayList.Commands, right.DisplayList.Commands)
}

func sameCommandSlice(left, right []Command) bool {
	if len(left) != len(right) {
		return false
	}
	if len(left) == 0 {
		return true
	}
	return &left[0] == &right[0]
}

func retainedUnderlayBoundariesEquivalent(commands []Command, left, right int) bool {
	if left <= 0 || right <= 0 || left > len(commands) || right > len(commands) {
		return false
	}
	for index := min(left, right); index < max(left, right); index++ {
		if commandPaintsPixels(commands[index].Kind) {
			return false
		}
	}
	return true
}

// rasterizeFullFrameLocked refreshes a learned scrolling underlay as part of
// the ordinary full replay. Capturing the prefix at the overlay boundary avoids
// painting the document a second time after artifact frames.
func (rasterizer *FrameRasterizer) rasterizeFullFrameLocked(frame *Frame, scale float64, viewport Viewport) (*image.RGBA, error) {
	rasterizer.overlayPaint.prepare(frame.paintArtifact)
	// A paint artifact may be projected into several display lists while
	// scrolling. A full replay has not proven any retained suffix unchanged,
	// so discard pre-opacity pixels even when no underlay boundary is reusable.
	rasterizer.overlayPaint.layers.clear()
	start, ok := retainedOverlayStart(frame.DisplayList.Commands, rasterizer.scrollOverlay, rasterizer.scrollBaseStart)
	if !ok {
		rasterizer.clearRetainedUnderlayLocked()
		return rasterizeAtScale(frame.DisplayList, rasterizer.fonts, scale, viewport)
	}
	canvas, underlay, err := rasterizeAtScaleWithUnderlayCapture(
		frame.DisplayList, rasterizer.fonts, scale, viewport, start, &rasterizer.overlayPaint,
	)
	if err != nil {
		return nil, err
	}
	rasterizer.scrollBase = underlay
	rasterizer.scrollBaseFrame = frame
	rasterizer.scrollBaseScale = scale
	rasterizer.scrollBaseStart = start
	return canvas, nil
}

func (rasterizer *FrameRasterizer) clearRetainedUnderlayLocked() {
	rasterizer.scrollBase = nil
	rasterizer.scrollBaseFrame = nil
	rasterizer.scrollBaseScale = 0
	rasterizer.scrollBaseStart = 0
}

func (rasterizer *FrameRasterizer) fullRasterUpdateLocked(frame *Frame, scale float64, viewport Viewport, fallback RasterFallbackReason) (RasterUpdate, error) {
	canvas, err := rasterizer.rasterizeFullFrameLocked(frame, scale, viewport)
	if err != nil {
		return RasterUpdate{}, err
	}
	return RasterUpdate{
		Canvas: canvas, DamagePixels: viewport.Width * viewport.Height,
		DamageRects: 1, Fallback: fallback,
	}, nil
}

func planScrollDamage(previous *image.RGBA, oldFrame, newFrame *Frame, scale float64, viewport Viewport) (scrollDamagePlan, RasterFallbackReason) {
	deviceBounds := image.Rect(0, 0, viewport.Width, viewport.Height)
	if previous == nil || previous.Bounds() != deviceBounds || oldFrame == nil {
		return scrollDamagePlan{}, RasterFallbackInvalidPrevious
	}
	if oldFrame.paintArtifact == 0 || oldFrame.paintArtifact != newFrame.paintArtifact ||
		!sameFrameFonts(oldFrame.Fonts, newFrame.Fonts) {
		return scrollDamagePlan{}, RasterFallbackArtifact
	}
	if oldFrame.DisplayList.Viewport != newFrame.DisplayList.Viewport ||
		oldFrame.DisplayList.Viewport != oldFrame.Viewport ||
		newFrame.DisplayList.Viewport != newFrame.Viewport ||
		len(oldFrame.DisplayList.Commands) != len(newFrame.DisplayList.Commands) {
		return scrollDamagePlan{}, RasterFallbackTopology
	}

	motions := make([]projectedCommandMotion, len(oldFrame.DisplayList.Commands))
	counts := make(map[image.Point]int)
	for index := range motions {
		motion, fallback := compareProjectedCommand(
			oldFrame.DisplayList.Commands[index], newFrame.DisplayList.Commands[index], scale, deviceBounds,
		)
		if fallback != "" {
			return scrollDamagePlan{}, fallback
		}
		motions[index] = motion
		if motion.painted && motion.delta != (image.Point{}) && !(motion.oldBounds.Empty() && motion.newBounds.Empty()) {
			counts[motion.delta]++
		}
	}
	qualifyUniformFillMotions(oldFrame.DisplayList.Commands, motions)
	if len(counts) == 0 {
		return scrollDamagePlan{}, RasterFallbackNoScroll
	}

	candidate := dominantScrollDelta(counts)
	overlayStart := stationaryOverlayStart(oldFrame.DisplayList.Commands, motions)
	if overlayStart > 0 && !unchangedOverlaySuffix(oldFrame.DisplayList.Commands, newFrame.DisplayList.Commands, overlayStart) {
		overlayStart = 0
	}
	movingCommands := 0
	for _, count := range counts {
		movingCommands += count
	}
	if !transformGroupsSupportDelta(motions, candidate, scale) {
		return scrollDamagePlan{}, RasterFallbackUnsafeGroup
	}
	distinct := 0
	for delta := range counts {
		distinct++
		if delta != candidate && !isStickyLikeDelta(delta, candidate) {
			return scrollDamagePlan{}, RasterFallbackMultipleScroll
		}
	}
	if distinct > 3 {
		return scrollDamagePlan{}, RasterFallbackMultipleScroll
	}

	clip, ok := outerScrollClip(motions, candidate, deviceBounds)
	if !ok {
		return scrollDamagePlan{}, RasterFallbackMultipleScroll
	}
	for _, motion := range motions {
		if motion.delta != candidate || !motion.painted || motion.oldBounds.Empty() && motion.newBounds.Empty() {
			continue
		}
		for _, stationary := range motion.stationaryClips {
			stationary = stationary.Intersect(deviceBounds)
			if stationary.Empty() {
				return scrollDamagePlan{}, RasterFallbackTopology
			}
			if !rectangleContains(clip, stationary) {
				return scrollDamagePlan{}, RasterFallbackMultipleScroll
			}
		}
	}
	if clip.Empty() || absInt(candidate.X) >= clip.Dx() || absInt(candidate.Y) >= clip.Dy() {
		return scrollDamagePlan{}, RasterFallbackExcessiveDamage
	}

	exposedDamage := exposedScrollDamage(clip, candidate)
	transformDamage := transformGroupRepairDamage(
		oldFrame.DisplayList.Commands, newFrame.DisplayList.Commands, motions,
		candidate, scale, deviceBounds, clip,
	)
	damage := append([]image.Rectangle(nil), exposedDamage...)
	damage = append(damage, transformDamage...)
	sources := []damageSourceRectangles{
		{name: "exposed", rectangles: exposedDamage},
		{name: "transform", rectangles: transformDamage},
	}
	for index, motion := range motions {
		if motion.delta == candidate && motion.stationaryRounded ||
			motion.roundedGroup && motion.delta != candidate && roundedClipGroupContainsDelta(motions, index, candidate) {
			return scrollDamagePlan{}, RasterFallbackUnsafeGroup
		}
		if !motion.painted {
			continue
		}
		if overlayStart > 0 && index >= overlayStart {
			continue
		}
		if motion.delta == candidate {
			if clip != deviceBounds && (!rectangleContains(clip, motion.oldBounds) || !rectangleContains(clip, motion.newBounds)) {
				return scrollDamagePlan{}, RasterFallbackMultipleScroll
			}
			if motion.hasCommandClip {
				repair := rectangularClipScrollDamage(
					motion.commandClip, motion.oldBounds, motion.newBounds, clip, candidate,
				)
				damage = append(damage, repair...)
				sources = appendDamageSource(sources, "clip_"+commandKindName(motion.kind), repair)
			}
			croppedVectorOrigin := (motion.roundedFill || motion.kind == FillEllipseCommand) &&
				motion.oldBounds.Add(candidate).Min != motion.newBounds.Min
			if !motion.requiresReplay && !croppedVectorOrigin {
				continue
			}
		}
		if motion.uniformFill && rectangleContains(motion.uniformOldBounds, clip) && rectangleContains(motion.uniformNewBounds, clip) {
			continue
		}
		if motion.delta != (image.Point{}) &&
			(!rectangleContains(clip, motion.oldBounds) || !rectangleContains(clip, motion.newBounds)) {
			return scrollDamagePlan{}, RasterFallbackMultipleScroll
		}
		if motion.roundedFill && motion.fillContextSafe &&
			!oldFrame.DisplayList.Commands[index].Radii.empty() {
			repair := roundedFillRepairDamage(
				oldFrame.DisplayList.Commands[index], newFrame.DisplayList.Commands[index],
				motion.uniformOldBounds, motion.uniformNewBounds, clip, candidate, scale,
			)
			damage = append(damage, repair...)
			sources = appendDamageSource(sources, "independent_fill_rect_rounded_boundary", repair)
			continue
		}
		assumed := motion.oldBounds.Add(candidate)
		repair := []image.Rectangle{
			expandDamageRect(motion.oldBounds, textDamagePadding).Intersect(clip),
			expandDamageRect(motion.newBounds, textDamagePadding).Intersect(clip),
			expandDamageRect(assumed, textDamagePadding).Intersect(clip),
		}
		damage = append(damage, repair...)
		if motion.delta == candidate {
			sources = appendDamageSource(sources, "replay_"+commandKindName(motion.kind), repair)
		} else {
			name := "independent_" + commandKindName(motion.kind)
			if motion.fillBlocker != "" {
				name += "_" + motion.fillBlocker
			}
			sources = appendDamageSource(sources, name, repair)
		}
	}

	rawDamage := append([]image.Rectangle(nil), damage...)
	damage = mergeDamageRectangles(damage, deviceBounds)
	if len(damage) == 0 {
		return scrollDamagePlan{}, RasterFallbackNoScroll
	}
	damagePixels := 0
	for _, rectangle := range damage {
		damagePixels += rectangleArea(rectangle)
	}
	plan := scrollDamagePlan{
		clip: clip, delta: candidate, damage: damage, damagePixels: damagePixels,
		rawDamageRects: len(rawDamage), scrollCommands: counts[candidate], movingCommands: movingCommands,
		overlayStart: overlayStart,
	}
	plan.damageSource, plan.sourceDamagePixels, plan.sourceDamageRects = dominantDamageSource(deviceBounds, sources)
	if len(damage) > maxRasterDamageRects || float64(damagePixels) > float64(rectangleArea(deviceBounds))*maxRasterDamageRatio {
		exact := exactDamageRectangles(rawDamage, deviceBounds)
		plan.exactDamageRects = len(exact)
		for _, rectangle := range exact {
			plan.exactDamagePixels += rectangleArea(rectangle)
		}
		return plan, RasterFallbackExcessiveDamage
	}
	return plan, ""
}

type damageSourceRectangles struct {
	name       string
	rectangles []image.Rectangle
}

func appendDamageSource(sources []damageSourceRectangles, name string, rectangles []image.Rectangle) []damageSourceRectangles {
	for index := range sources {
		if sources[index].name == name {
			sources[index].rectangles = append(sources[index].rectangles, rectangles...)
			return sources
		}
	}
	return append(sources, damageSourceRectangles{name: name, rectangles: append([]image.Rectangle(nil), rectangles...)})
}

func dominantDamageSource(bounds image.Rectangle, sources []damageSourceRectangles) (string, int, int) {
	var dominant string
	dominantPixels, dominantRects := 0, 0
	for _, source := range sources {
		exact := exactDamageRectangles(source.rectangles, bounds)
		pixels := 0
		for _, rectangle := range exact {
			pixels += rectangleArea(rectangle)
		}
		if pixels > dominantPixels {
			dominant, dominantPixels, dominantRects = source.name, pixels, len(exact)
		}
	}
	return dominant, dominantPixels, dominantRects
}

func compareProjectedCommand(oldCommand, newCommand Command, scale float64, viewport image.Rectangle) (projectedCommandMotion, RasterFallbackReason) {
	motion := projectedCommandMotion{kind: oldCommand.Kind}
	if difference := commandPayloadDifference(oldCommand, newCommand); difference != "" {
		return motion, RasterFallbackReason("payload_" + commandKindName(oldCommand.Kind) + "_" + difference)
	}
	if oldCommand.Kind == BeginTransformCommand {
		motion.transformBegin = true
		motion.oldTransform = oldCommand.Transform
		motion.newTransform = newCommand.Transform
	}
	motion.painted = commandPaintsPixels(oldCommand.Kind)
	motion.roundedGroup = oldCommand.Kind == BeginClipCommand && !oldCommand.Radii.empty()

	var delta image.Point
	var ok bool
	switch oldCommand.Kind {
	case FillRectCommand, FillEllipseCommand, DrawImageCommand, DrawRoundedBorderCommand, DrawBoxShadowCommand, DrawGradientCommand, BeginClipCommand:
		delta, ok = deviceCoordinateDelta(oldCommand.Rect.X, oldCommand.Rect.Y, newCommand.Rect.X, newCommand.Rect.Y, scale)
	case DrawTextCommand:
		delta, ok = deviceCoordinateDelta(oldCommand.X, oldCommand.BaselineY, newCommand.X, newCommand.BaselineY, scale)
		if ok {
			ok = positionHasDelta(oldCommand.textBounds.X, oldCommand.textBounds.Y, newCommand.textBounds.X, newCommand.textBounds.Y, scale, delta)
		}
		if ok && oldCommand.textOrientation != textPaintHorizontal {
			ok = positionHasDelta(oldCommand.Rect.X, oldCommand.Rect.Y, newCommand.Rect.X, newCommand.Rect.Y, scale, delta)
		}
	default:
		ok = true
	}
	if !ok {
		return motion, RasterFallbackFractional
	}
	motion.delta = delta

	if oldCommand.HasClip {
		if !finiteScaledRect(oldCommand.Clip, scale) || !finiteScaledRect(newCommand.Clip, scale) {
			return motion, RasterFallbackClipMotion
		}
		motion.commandClip = projectedRectangularClip{
			old: scaledRasterRect(oldCommand.Clip, scale).Intersect(viewport),
			new: scaledRasterRect(newCommand.Clip, scale).Intersect(viewport),
		}
		motion.hasCommandClip = true
		if delta != (image.Point{}) && motion.commandClip.old.Min == motion.commandClip.new.Min {
			// A stationary clip that changes size still bounds the candidate
			// scroll copy. Use the union so both old pixels and newly admitted
			// pixels are inside the region that will be repaired.
			motion.stationaryClips = append(motion.stationaryClips, motion.commandClip.old.Union(motion.commandClip.new))
		}
	}
	for index := 0; index < int(oldCommand.RoundedClipCount); index++ {
		oldClip, newClip := oldCommand.RoundedClips[index], newCommand.RoundedClips[index]
		clipDelta, valid := deviceCoordinateDelta(oldClip.Rect.X, oldClip.Rect.Y, newClip.Rect.X, newClip.Rect.Y, scale)
		if !valid || clipDelta != delta && clipDelta != (image.Point{}) {
			return motion, RasterFallbackClipMotion
		}
		if delta != (image.Point{}) && clipDelta == (image.Point{}) {
			motion.stationaryRounded = true
		}
	}

	if motion.painted {
		motion.oldBounds = scaledCommandBounds(oldCommand, scale, viewport)
		motion.newBounds = scaledCommandBounds(newCommand, scale, viewport)
		if oldCommand.Kind == FillRectCommand {
			switch {
			case oldCommand.RoundedClipCount != 0:
				motion.fillBlocker = "rounded_clip"
			default:
				motion.fillContextSafe = true
				motion.uniformOldBounds = motion.oldBounds
				motion.uniformNewBounds = motion.newBounds
				if oldCommand.Radii.empty() {
					motion.uniformFill = true
				} else {
					motion.roundedFill = true
					if oldCommand.Color.A == 0xff {
						motion.fillBlocker = "rounded"
					} else {
						motion.fillBlocker = "alpha_rounded"
					}
				}
			}
		}
	} else if oldCommand.Kind == BeginClipCommand {
		motion.oldBounds = scaledRasterRect(oldCommand.Rect, scale).Intersect(viewport)
		motion.newBounds = scaledRasterRect(newCommand.Rect, scale).Intersect(viewport)
	}
	return motion, ""
}

func qualifyUniformFillMotions(commands []Command, motions []projectedCommandMotion) {
	opacityDepth, transformDepth := 0, 0
	var opacityStack []bool
	var clipStack []projectedCommandMotion
	for index, command := range commands {
		switch command.Kind {
		case EndOpacityCommand:
			if last := len(opacityStack) - 1; last >= 0 {
				active := opacityStack[last]
				opacityStack = opacityStack[:last]
				if active {
					opacityDepth--
				}
			}
		case EndTransformCommand:
			if transformDepth > 0 {
				transformDepth--
			}
		case EndClipCommand:
			if last := len(clipStack) - 1; last >= 0 {
				clipStack = clipStack[:last]
			}
		}
		if motions[index].uniformFill || motions[index].roundedFill {
			if transformDepth > 0 {
				motions[index].uniformFill = false
				motions[index].fillContextSafe = false
				motions[index].fillBlocker = "transform_group"
			} else {
				for _, group := range clipStack {
					if group.roundedGroup || group.delta != (image.Point{}) || group.oldBounds != group.newBounds {
						motions[index].uniformFill = false
						motions[index].fillContextSafe = false
						motions[index].fillBlocker = "clip_group"
						break
					}
					motions[index].uniformOldBounds = motions[index].uniformOldBounds.Intersect(group.oldBounds)
					motions[index].uniformNewBounds = motions[index].uniformNewBounds.Intersect(group.newBounds)
				}
			}
			// A group opacity changes the blend result of a rectangular fill, so
			// its interior cannot be skipped as a uniform overdraw. Rounded fills
			// remain boundary-repairable: the group is replayed in stable layer
			// coordinates and only its curved coverage differs after the copy.
			if motions[index].fillContextSafe && opacityDepth > 0 {
				motions[index].uniformFill = false
				motions[index].fillBlocker = "opacity_group"
			}
		}
		switch command.Kind {
		case BeginOpacityCommand:
			active := command.Opacity < 1
			opacityStack = append(opacityStack, active)
			if active {
				opacityDepth++
			}
		case BeginTransformCommand:
			transformDepth++
		case BeginClipCommand:
			clipStack = append(clipStack, motions[index])
		}
	}
}

// stationaryOverlayStart returns a group-safe paint boundary after the last
// moving command. Pixel commands at and after the boundary can be replayed as
// a stationary overlay on a separately retained scrolling underlay. Direct
// rectangular clips may span the boundary; opacity, transforms, and rounded
// clips may not because splitting their intermediate surface changes blending.
func stationaryOverlayStart(commands []Command, motions []projectedCommandMotion) int {
	lastDynamic := -1
	for index, motion := range motions {
		if motion.painted && motion.delta != (image.Point{}) {
			lastDynamic = index
		}
	}
	if lastDynamic < 0 {
		return 0
	}

	state := rasterSplitState{}
	candidate := -1
	hasOverlayPaint := false
	for index, command := range commands {
		if index > lastDynamic && candidate < 0 && state.safe() {
			candidate = index
		}
		if candidate >= 0 && index >= candidate && motions[index].painted {
			hasOverlayPaint = true
		}
		state.advance(command)
	}
	if candidate <= 0 || !hasOverlayPaint {
		return 0
	}
	return candidate
}

func overlayIdentityAt(commands []Command, start int) retainedOverlayIdentity {
	for index := max(0, start); index < len(commands); index++ {
		command := commands[index]
		if commandPaintsPixels(command.Kind) && command.Node != nil {
			return retainedOverlayCommandIdentity(command)
		}
	}
	return retainedOverlayIdentity{}
}

func retainedOverlayCommandIdentity(command Command) retainedOverlayIdentity {
	identity := retainedOverlayIdentity{node: command.Node, image: command.Image, hasCommand: true}
	command.Node = nil
	command.Image = nil
	identity.command = command
	return identity
}

func (identity retainedOverlayIdentity) matches(candidate retainedOverlayIdentity) bool {
	if identity.node != nil && identity.node == candidate.node && identity.command.Kind == candidate.command.Kind {
		return true
	}
	return identity.hasCommand && candidate.hasCommand && identity.command == candidate.command &&
		sameImageIdentity(identity.image, candidate.image)
}

func unchangedOverlaySuffix(oldCommands, newCommands []Command, start int) bool {
	if start <= 0 || len(oldCommands) != len(newCommands) {
		return false
	}
	for index := start; index < len(oldCommands); index++ {
		oldCommand, newCommand := oldCommands[index], newCommands[index]
		if !sameImageIdentity(oldCommand.Image, newCommand.Image) {
			return false
		}
		oldCommand.Image = nil
		newCommand.Image = nil
		if oldCommand != newCommand {
			return false
		}
	}
	return true
}

func retainedOverlayStart(commands []Command, identity retainedOverlayIdentity, preferred int) (int, bool) {
	if identity.node == nil && !identity.hasCommand {
		return 0, false
	}
	if preferred > 0 && preferred < len(commands) && rasterSplitSafeAt(commands, preferred) {
		if candidate := overlayIdentityAt(commands, preferred); identity.matches(candidate) {
			return preferred, true
		}
	}
	state := rasterSplitState{}
	lastSafe := 0
	for index, command := range commands {
		if state.safe() {
			lastSafe = index
		}
		if commandPaintsPixels(command.Kind) {
			candidate := retainedOverlayCommandIdentity(command)
			if identity.matches(candidate) {
				return lastSafe, lastSafe > 0
			}
		}
		state.advance(command)
	}
	return 0, false
}

func rasterSplitSafeAt(commands []Command, boundary int) bool {
	if boundary <= 0 || boundary > len(commands) {
		return false
	}
	state := rasterSplitState{}
	for index := 0; index < boundary; index++ {
		state.advance(commands[index])
	}
	return state.safe()
}

type rasterSplitState struct {
	opacityDepth     int
	transformDepth   int
	roundedClipDepth int
	clipStack        []bool
}

func (state *rasterSplitState) safe() bool {
	return state.opacityDepth == 0 && state.transformDepth == 0 && state.roundedClipDepth == 0
}

func (state *rasterSplitState) advance(command Command) {
	switch command.Kind {
	case BeginOpacityCommand:
		state.opacityDepth++
	case EndOpacityCommand:
		if state.opacityDepth > 0 {
			state.opacityDepth--
		}
	case BeginTransformCommand:
		state.transformDepth++
	case EndTransformCommand:
		if state.transformDepth > 0 {
			state.transformDepth--
		}
	case BeginClipCommand:
		rounded := !command.Radii.empty()
		state.clipStack = append(state.clipStack, rounded)
		if rounded {
			state.roundedClipDepth++
		}
	case EndClipCommand:
		if last := len(state.clipStack) - 1; last >= 0 {
			if state.clipStack[last] && state.roundedClipDepth > 0 {
				state.roundedClipDepth--
			}
			state.clipStack = state.clipStack[:last]
		}
	}
}

// outerScrollClip chooses the single largest stationary rectangular clip that
// contains every visible command moving by the candidate delta. A command with
// no stationary clip is rooted in the device viewport. Nested clips remain
// per-command masks and are repaired at both shifted edges below.
func outerScrollClip(motions []projectedCommandMotion, candidate image.Point, deviceBounds image.Rectangle) (image.Rectangle, bool) {
	clips := make([]image.Rectangle, 0, len(motions))
	for _, motion := range motions {
		if !motion.painted || motion.delta != candidate || motion.oldBounds.Empty() && motion.newBounds.Empty() {
			continue
		}
		if len(motion.stationaryClips) == 0 {
			clips = append(clips, deviceBounds)
			continue
		}
		for _, clip := range motion.stationaryClips {
			clip = clip.Intersect(deviceBounds)
			if clip.Empty() {
				return image.Rectangle{}, false
			}
			clips = append(clips, clip)
		}
	}
	if len(clips) == 0 {
		return image.Rectangle{}, false
	}
	for _, possible := range clips {
		containsAll := true
		for _, clip := range clips {
			if !rectangleContains(possible, clip) {
				containsAll = false
				break
			}
		}
		if containsAll {
			return possible, true
		}
	}
	return image.Rectangle{}, false
}

func roundedClipGroupContainsDelta(motions []projectedCommandMotion, begin int, delta image.Point) bool {
	depth := 0
	for index := begin + 1; index < len(motions); index++ {
		switch motions[index].kind {
		case BeginClipCommand:
			depth++
		case EndClipCommand:
			if depth == 0 {
				return false
			}
			depth--
		default:
			if motions[index].painted && motions[index].delta == delta {
				return true
			}
		}
	}
	return true
}

func transformGroupsSupportDelta(motions []projectedCommandMotion, scroll image.Point, scale float64) bool {
	for begin, motion := range motions {
		if !motion.transformBegin {
			continue
		}
		depth := 0
		deltas := make(map[image.Point]struct{})
		closed := false
		for index := begin + 1; index < len(motions); index++ {
			switch motions[index].kind {
			case BeginTransformCommand:
				depth++
			case EndTransformCommand:
				if depth == 0 {
					closed = true
					index = len(motions)
					continue
				}
				depth--
			default:
				if motions[index].painted {
					deltas[motions[index].delta] = struct{}{}
				}
			}
		}
		if !closed || len(deltas) > 1 {
			return false
		}
		groupDelta := image.Point{}
		for delta := range deltas {
			groupDelta = delta
		}
		if groupDelta != scroll && groupDelta != (image.Point{}) {
			return false
		}
		if !transformProjectsTranslation(motion.oldTransform, motion.newTransform, groupDelta, scale) {
			return false
		}
	}
	return true
}

func transformGroupRepairDamage(oldCommands, newCommands []Command, motions []projectedCommandMotion, scroll image.Point, scale float64, viewport, clip image.Rectangle) []image.Rectangle {
	hasTransform := false
	for _, motion := range motions {
		if motion.transformBegin {
			hasTransform = true
			break
		}
	}
	if !hasTransform {
		return nil
	}
	oldBounds := rasterGroupBoundsAtScale(oldCommands, viewport, scale)
	newBounds := rasterGroupBoundsAtScale(newCommands, viewport, scale)
	var damage []image.Rectangle
	for begin, motion := range motions {
		if !motion.transformBegin {
			continue
		}
		groupDelta, ok := oneTransformGroupDelta(motions, begin)
		if !ok || (groupDelta == scroll && oldBounds[begin].Add(scroll) == newBounds[begin]) {
			continue
		}
		oldTransform, newTransform := oldCommands[begin], newCommands[begin]
		scaleRasterCommand(&oldTransform, scale)
		scaleRasterCommand(&newTransform, scale)
		oldOutput := transformedGroupBounds(oldBounds[begin], oldTransform.Transform).Intersect(viewport)
		newOutput := transformedGroupBounds(newBounds[begin], newTransform.Transform).Intersect(viewport)
		if !oldOutput.Overlaps(clip) && !newOutput.Overlaps(clip) && !oldOutput.Add(scroll).Overlaps(clip) {
			continue
		}
		damage = append(damage, oldOutput.Intersect(clip), newOutput.Intersect(clip), oldOutput.Add(scroll).Intersect(clip))
	}
	return damage
}

func oneTransformGroupDelta(motions []projectedCommandMotion, begin int) (image.Point, bool) {
	depth := 0
	var delta image.Point
	haveDelta := false
	for index := begin + 1; index < len(motions); index++ {
		switch motions[index].kind {
		case BeginTransformCommand:
			depth++
		case EndTransformCommand:
			if depth == 0 {
				return delta, true
			}
			depth--
		default:
			if !motions[index].painted {
				continue
			}
			if !haveDelta {
				delta, haveDelta = motions[index].delta, true
			} else if motions[index].delta != delta {
				return image.Point{}, false
			}
		}
	}
	return image.Point{}, false
}

func transformedGroupBounds(source image.Rectangle, transform AffineTransform) image.Rectangle {
	if source.Empty() {
		return image.Rectangle{}
	}
	return rectImageBounds(transformRasterRect(Rect{
		X: float64(source.Min.X), Y: float64(source.Min.Y),
		Width: float64(source.Dx()), Height: float64(source.Dy()),
	}, transform))
}

func transformProjectsTranslation(oldTransform, newTransform AffineTransform, delta image.Point, scale float64) bool {
	if oldTransform.A != newTransform.A || oldTransform.B != newTransform.B ||
		oldTransform.C != newTransform.C || oldTransform.D != newTransform.D {
		return false
	}
	wantE := oldTransform.E*scale + float64(delta.X) - oldTransform.A*float64(delta.X) - oldTransform.C*float64(delta.Y)
	wantF := oldTransform.F*scale + float64(delta.Y) - oldTransform.B*float64(delta.X) - oldTransform.D*float64(delta.Y)
	return math.Abs(newTransform.E*scale-wantE) <= 1e-7 && math.Abs(newTransform.F*scale-wantF) <= 1e-7
}

func commandPayloadDifference(left, right Command) string {
	transformsMatch := left.Transform == right.Transform
	if left.Kind == BeginTransformCommand && right.Kind == BeginTransformCommand {
		transformsMatch = left.Transform.A == right.Transform.A && left.Transform.B == right.Transform.B &&
			left.Transform.C == right.Transform.C && left.Transform.D == right.Transform.D
	}
	if left.Kind != right.Kind || left.Node != right.Node || left.Pseudo != right.Pseudo {
		return "identity"
	}
	if left.Rect.Width != right.Rect.Width || left.Rect.Height != right.Rect.Height {
		return "rect"
	}
	if left.Color != right.Color || left.Radii != right.Radii || left.BorderWidths != right.BorderWidths ||
		left.BorderColors != right.BorderColors || left.BorderStyles != right.BorderStyles {
		return "decoration"
	}
	if left.ShadowOffsetX != right.ShadowOffsetX || left.ShadowOffsetY != right.ShadowOffsetY ||
		left.ShadowBlur != right.ShadowBlur || left.ShadowSpread != right.ShadowSpread || left.ShadowInset != right.ShadowInset {
		return "shadow"
	}
	if left.GradientKind != right.GradientKind || left.GradientAngle != right.GradientAngle ||
		left.GradientRepeating != right.GradientRepeating || left.GradientStops != right.GradientStops ||
		left.GradientStopCount != right.GradientStopCount {
		return "gradient"
	}
	if !transformsMatch {
		return "transform"
	}
	if left.Text != right.Text || left.FontSize != right.FontSize || left.FontFamily != right.FontFamily ||
		left.FontWeight != right.FontWeight || left.FontStyle != right.FontStyle || left.WebFont != right.WebFont {
		return "text"
	}
	if left.Opacity != right.Opacity {
		return "opacity"
	}
	if left.HasClip != right.HasClip {
		return "clip_presence"
	}
	if left.textOrientation != right.textOrientation || left.textBounds.Width != right.textBounds.Width ||
		left.textBounds.Height != right.textBounds.Height || left.textWidth != right.textWidth ||
		left.textHeight != right.textHeight || left.textBaseline != right.textBaseline {
		return "text_metrics"
	}
	if left.RoundedClipCount != right.RoundedClipCount {
		return "rounded_clip_count"
	}
	if !sameImageIdentity(left.Image, right.Image) {
		return "image"
	}
	// Rectangular command-local clips are safe to resize incrementally: the
	// planner records old and new device geometry and repairs their symmetric
	// difference after the scroll copy. A command with a rounded mask remains
	// conservative because coverage at the intersecting curved edge is not a
	// rectangular damage problem.
	if left.HasClip && left.RoundedClipCount > 0 &&
		(left.Clip.Width != right.Clip.Width || left.Clip.Height != right.Clip.Height) {
		return "clip_size"
	}
	for index := 0; index < int(left.RoundedClipCount); index++ {
		leftClip, rightClip := left.RoundedClips[index], right.RoundedClips[index]
		if leftClip.Rect.Width != rightClip.Rect.Width || leftClip.Rect.Height != rightClip.Rect.Height || leftClip.Radii != rightClip.Radii {
			return "rounded_clip"
		}
	}
	return ""
}

func commandKindName(kind CommandKind) string {
	switch kind {
	case FillRectCommand:
		return "fill_rect"
	case FillEllipseCommand:
		return "fill_ellipse"
	case DrawTextCommand:
		return "draw_text"
	case BeginOpacityCommand:
		return "begin_opacity"
	case EndOpacityCommand:
		return "end_opacity"
	case DrawImageCommand:
		return "draw_image"
	case DrawRoundedBorderCommand:
		return "draw_rounded_border"
	case DrawBoxShadowCommand:
		return "draw_box_shadow"
	case DrawGradientCommand:
		return "draw_gradient"
	case BeginTransformCommand:
		return "begin_transform"
	case EndTransformCommand:
		return "end_transform"
	case BeginClipCommand:
		return "begin_clip"
	case EndClipCommand:
		return "end_clip"
	default:
		return "unknown"
	}
}

func sameImageIdentity(left, right image.Image) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftType, rightType := reflect.TypeOf(left), reflect.TypeOf(right)
	if leftType != rightType || !leftType.Comparable() {
		return false
	}
	return left == right
}

func sameFrameFonts(left, right []WebFontFace) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !sameWebFont(identifyWebFont(left[index]), right[index]) {
			return false
		}
	}
	return true
}

func commandPaintsPixels(kind CommandKind) bool {
	switch kind {
	case FillRectCommand, FillEllipseCommand, DrawTextCommand, DrawImageCommand,
		DrawRoundedBorderCommand, DrawBoxShadowCommand, DrawGradientCommand:
		return true
	default:
		return false
	}
}

func deviceCoordinateDelta(oldX, oldY, newX, newY, scale float64) (image.Point, bool) {
	x, xOK := integerDeviceDelta((newX - oldX) * scale)
	y, yOK := integerDeviceDelta((newY - oldY) * scale)
	return image.Pt(x, y), xOK && yOK
}

func positionHasDelta(oldX, oldY, newX, newY, scale float64, want image.Point) bool {
	got, ok := deviceCoordinateDelta(oldX, oldY, newX, newY, scale)
	return ok && got == want
}

func integerDeviceDelta(value float64) (int, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	rounded := math.Round(value)
	if math.Abs(value-rounded) > 1e-7 || rounded < float64(math.MinInt32) || rounded > float64(math.MaxInt32) {
		return 0, false
	}
	return int(rounded), true
}

func scaledCommandBounds(command Command, scale float64, viewport image.Rectangle) image.Rectangle {
	scaleRasterCommand(&command, scale)
	bounds, valid := rasterCommandBounds(command, viewport)
	if !valid {
		return image.Rectangle{}
	}
	result := rectImageBounds(bounds).Intersect(viewport)
	if command.Kind == DrawTextCommand {
		padding := max(textDamagePadding, int(math.Ceil(command.FontSize))+2)
		result = expandDamageRect(result, padding).Intersect(viewport)
		if command.HasClip {
			result = result.Intersect(rectImageBounds(command.Clip))
		}
		for index := 0; index < int(command.RoundedClipCount); index++ {
			result = result.Intersect(rectImageBounds(command.RoundedClips[index].Rect))
		}
	}
	return result
}

func scaledRasterRect(rectangle Rect, scale float64) image.Rectangle {
	return rectImageBounds(scaleRect(rectangle, scale))
}

func finiteScaledRect(rectangle Rect, scale float64) bool {
	return isFinite(rectangle.X*scale) && isFinite(rectangle.Y*scale) &&
		isFinite(rectangle.Width*scale) && isFinite(rectangle.Height*scale)
}

func dominantScrollDelta(counts map[image.Point]int) image.Point {
	var result image.Point
	resultCount, resultMagnitude := -1, -1
	for delta, count := range counts {
		magnitude := absInt(delta.X) + absInt(delta.Y)
		if count > resultCount || count == resultCount && magnitude > resultMagnitude {
			result, resultCount, resultMagnitude = delta, count, magnitude
		}
	}
	return result
}

func isStickyLikeDelta(delta, scroll image.Point) bool {
	axisWithin := func(value, limit int) bool {
		if limit >= 0 {
			return value >= 0 && value <= limit
		}
		return value <= 0 && value >= limit
	}
	return axisWithin(delta.X, scroll.X) && axisWithin(delta.Y, scroll.Y)
}

func exposedScrollDamage(clip image.Rectangle, delta image.Point) []image.Rectangle {
	if clip.Empty() {
		return nil
	}
	source := clip.Intersect(clip.Sub(delta))
	destination := source.Add(delta)
	return subtractRectangle(clip, destination)
}

// rectangularClipScrollDamage repairs the pixels whose command-local clip
// membership changed across the retained scroll copy. The copied old pixels
// are masked by old.Add(scroll); the target pixels are masked by new. Their
// symmetric difference covers both newly exposed content and stale ghosts.
func rectangularClipScrollDamage(clip projectedRectangularClip, oldBounds, newBounds, outer image.Rectangle, scroll image.Point) []image.Rectangle {
	// Clip-mask changes cannot affect pixels outside the command's copied-old
	// or target-new painted bounds. Limiting repair to that union avoids turning
	// a tiny clipped item into viewport-sized damage just because an ancestor
	// scrollport is large.
	affected := oldBounds.Add(scroll).Union(newBounds).Intersect(outer)
	shiftedOld := clip.old.Add(scroll).Intersect(affected)
	newClip := clip.new.Intersect(affected)
	if shiftedOld == newClip {
		return nil
	}
	damage := append([]image.Rectangle(nil), subtractRectangle(shiftedOld, newClip)...)
	damage = append(damage, subtractRectangle(newClip, shiftedOld)...)
	return damage
}

// roundedFillRepairDamage keeps the translation-invariant interior of a
// stationary rounded fill. After the framebuffer scroll copy, only the
// old/target shape symmetric difference and the curved corner coverage can
// differ. The difference strips extend one device pixel into their shared
// interior so fractional straight-edge antialiasing is repaired too. Replaying
// those strips and padded corner boxes avoids turning a large card or panel
// interior into damage. This also applies to translucent fills because replay
// uses the stable full-layer vector origin.
func roundedFillRepairDamage(oldCommand, newCommand Command, oldCoverage, newCoverage, outer image.Rectangle, scroll image.Point, scale float64) []image.Rectangle {
	copiedCoverage := oldCoverage.Add(scroll).Intersect(outer)
	targetCoverage := newCoverage.Intersect(outer)
	var damage []image.Rectangle
	for _, rectangle := range subtractRectangle(copiedCoverage, targetCoverage) {
		damage = appendNonEmptyRectangle(damage, expandDamageRect(rectangle, 1).Intersect(outer))
	}
	for _, rectangle := range subtractRectangle(targetCoverage, copiedCoverage) {
		damage = appendNonEmptyRectangle(damage, expandDamageRect(rectangle, 1).Intersect(outer))
	}

	oldRadii := scaleCornerRadii(oldCommand.Radii, scale)
	newRadii := scaleCornerRadii(newCommand.Radii, scale)
	oldBounds := scaledRasterRect(oldCommand.Rect, scale).Add(scroll)
	newBounds := scaledRasterRect(newCommand.Rect, scale)
	damage = append(damage, roundedFillCornerDamage(oldBounds, oldRadii, outer)...)
	damage = append(damage, roundedFillCornerDamage(newBounds, newRadii, outer)...)
	return damage
}

func roundedFillCornerDamage(bounds image.Rectangle, radii CornerRadii, outer image.Rectangle) []image.Rectangle {
	const padding = 2
	type corner struct {
		radius CornerRadius
		right  bool
		bottom bool
	}
	corners := [...]corner{
		{radius: radii.TopLeft},
		{radius: radii.TopRight, right: true},
		{radius: radii.BottomRight, right: true, bottom: true},
		{radius: radii.BottomLeft, bottom: true},
	}
	result := make([]image.Rectangle, 0, len(corners))
	for _, current := range corners {
		radiusX := int(math.Ceil(max(0, current.radius.X)))
		radiusY := int(math.Ceil(max(0, current.radius.Y)))
		if radiusX == 0 || radiusY == 0 {
			continue
		}
		minX, maxX := bounds.Min.X-padding, bounds.Min.X+radiusX+padding
		minY, maxY := bounds.Min.Y-padding, bounds.Min.Y+radiusY+padding
		if current.right {
			minX, maxX = bounds.Max.X-radiusX-padding, bounds.Max.X+padding
		}
		if current.bottom {
			minY, maxY = bounds.Max.Y-radiusY-padding, bounds.Max.Y+padding
		}
		result = appendNonEmptyRectangle(result, image.Rect(minX, minY, maxX, maxY).Intersect(outer))
	}
	return result
}

func subtractRectangle(outer, inner image.Rectangle) []image.Rectangle {
	inner = inner.Intersect(outer)
	if inner.Empty() {
		return []image.Rectangle{outer}
	}
	result := make([]image.Rectangle, 0, 4)
	result = appendNonEmptyRectangle(result, image.Rect(outer.Min.X, outer.Min.Y, outer.Max.X, inner.Min.Y))
	result = appendNonEmptyRectangle(result, image.Rect(outer.Min.X, inner.Max.Y, outer.Max.X, outer.Max.Y))
	result = appendNonEmptyRectangle(result, image.Rect(outer.Min.X, inner.Min.Y, inner.Min.X, inner.Max.Y))
	result = appendNonEmptyRectangle(result, image.Rect(inner.Max.X, inner.Min.Y, outer.Max.X, inner.Max.Y))
	return result
}

// exactDamageRectangles returns a disjoint representation of the geometric
// union without expanding overlapping L-shaped repairs into their bounding
// rectangle. The rasterizer intentionally continues to replay the conservative
// merged rectangles: this exact union is diagnostic data used to distinguish
// genuine viewport-sized damage from merge inflation before changing replay
// semantics.
func exactDamageRectangles(rectangles []image.Rectangle, bounds image.Rectangle) []image.Rectangle {
	result := make([]image.Rectangle, 0, len(rectangles))
	for _, rectangle := range rectangles {
		fragments := []image.Rectangle{rectangle.Intersect(bounds)}
		if fragments[0].Empty() {
			continue
		}
		for _, covered := range result {
			next := make([]image.Rectangle, 0, len(fragments)+3)
			for _, fragment := range fragments {
				next = append(next, subtractRectangle(fragment, covered)...)
			}
			fragments = next
			if len(fragments) == 0 {
				break
			}
		}
		result = append(result, fragments...)
	}
	return result
}

func appendNonEmptyRectangle(rectangles []image.Rectangle, rectangle image.Rectangle) []image.Rectangle {
	if !rectangle.Empty() {
		return append(rectangles, rectangle)
	}
	return rectangles
}

func mergeDamageRectangles(rectangles []image.Rectangle, bounds image.Rectangle) []image.Rectangle {
	result := make([]image.Rectangle, 0, len(rectangles))
	for _, rectangle := range rectangles {
		rectangle = rectangle.Intersect(bounds)
		if rectangle.Empty() {
			continue
		}
		for index := 0; index < len(result); {
			if !rectanglesTouch(rectangle, result[index]) {
				index++
				continue
			}
			rectangle = rectangle.Union(result[index])
			result = append(result[:index], result[index+1:]...)
			index = 0
		}
		result = append(result, rectangle)
	}
	return result
}

func rectanglesTouch(left, right image.Rectangle) bool {
	return left.Min.X < right.Max.X && right.Min.X < left.Max.X &&
		left.Min.Y < right.Max.Y && right.Min.Y < left.Max.Y
}

func copyScrollRectangle(canvas *image.RGBA, clip image.Rectangle, delta image.Point) {
	source := clip.Intersect(clip.Sub(delta)).Intersect(canvas.Bounds())
	destination := source.Add(delta).Intersect(canvas.Bounds())
	if source.Empty() || destination.Empty() {
		return
	}
	rowBytes := source.Dx() * 4
	copyRow := func(sourceY, destinationY int) {
		sourceOffset := canvas.PixOffset(source.Min.X, sourceY)
		destinationOffset := canvas.PixOffset(destination.Min.X, destinationY)
		copy(canvas.Pix[destinationOffset:destinationOffset+rowBytes], canvas.Pix[sourceOffset:sourceOffset+rowBytes])
	}
	if delta.Y > 0 {
		for row := source.Dy() - 1; row >= 0; row-- {
			copyRow(source.Min.Y+row, destination.Min.Y+row)
		}
		return
	}
	for row := 0; row < source.Dy(); row++ {
		copyRow(source.Min.Y+row, destination.Min.Y+row)
	}
}

func rectangleArea(rectangle image.Rectangle) int {
	if rectangle.Empty() {
		return 0
	}
	return rectangle.Dx() * rectangle.Dy()
}

func rectangleContains(outer, inner image.Rectangle) bool {
	return inner.Empty() || inner.Intersect(outer) == inner
}

func expandDamageRect(rectangle image.Rectangle, amount int) image.Rectangle {
	if rectangle.Empty() || amount <= 0 {
		return rectangle
	}
	return image.Rect(rectangle.Min.X-amount, rectangle.Min.Y-amount, rectangle.Max.X+amount, rectangle.Max.Y+amount)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// errorf is kept local so the hot update implementation imports no formatting
// package beyond the renderer's existing error construction path.
func errorf(message string) error { return &rasterUpdateError{message: message} }

type rasterUpdateError struct{ message string }

func (err *rasterUpdateError) Error() string { return err.message }
