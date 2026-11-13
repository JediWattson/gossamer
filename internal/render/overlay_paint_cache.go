package render

import (
	"image"
	"math"
	"sync"
	"sync/atomic"
)

const maxEffectLayerCacheBytes = 8 * 1024 * 1024

type rasterOverlayPaintCache struct {
	artifact PaintArtifactID
	coverage roundedCoverageCache
	layers   effectLayerCache
}

func (cache *rasterOverlayPaintCache) prepare(artifact PaintArtifactID) {
	if cache.artifact == artifact {
		return
	}
	cache.clear()
	cache.artifact = artifact
}

func (cache *rasterOverlayPaintCache) clear() {
	cache.artifact = 0
	cache.coverage.clear()
	cache.layers.clear()
}

type effectLayerCacheKey struct {
	kind       rasterLayerKind
	begin, end int
	scale      uint64
	bounds     image.Rectangle
	output     image.Rectangle
	opacity    uint8
	transform  [6]uint64
}

type effectLayerCacheEntry struct {
	rgba     *image.RGBA
	rgba64   *image.RGBA64
	bytes    int
	lastUsed atomic.Uint64
}

// effectLayerCache shares one memory cap between pre-opacity RGBA layers and
// 16-bit transformed samples. Entries are immutable after publication; the
// mutex protects concurrent overlay-region lookups and true LRU accounting.
type effectLayerCache struct {
	mutex   sync.RWMutex
	entries map[effectLayerCacheKey]*effectLayerCacheEntry
	bytes   int
	clock   atomic.Uint64
}

func opacityEffectLayerKey(begin, end int, scale float64, bounds image.Rectangle, opacity uint8) effectLayerCacheKey {
	return effectLayerCacheKey{
		kind: rasterOpacityLayer, begin: begin, end: end,
		scale: math.Float64bits(scale), bounds: bounds, opacity: opacity,
	}
}

func transformEffectLayerKey(begin, end int, scale float64, bounds, output image.Rectangle, transform AffineTransform) effectLayerCacheKey {
	return effectLayerCacheKey{
		kind: rasterTransformLayer, begin: begin, end: end,
		scale: math.Float64bits(scale), bounds: bounds, output: output,
		transform: [6]uint64{
			math.Float64bits(transform.A), math.Float64bits(transform.B),
			math.Float64bits(transform.C), math.Float64bits(transform.D),
			math.Float64bits(transform.E), math.Float64bits(transform.F),
		},
	}
}

func (cache *effectLayerCache) getOpacity(begin, end int, scale float64, bounds image.Rectangle, opacity uint8) (*image.RGBA, bool) {
	entry, ok := cache.get(opacityEffectLayerKey(begin, end, scale, bounds, opacity))
	if !ok || entry == nil || entry.rgba == nil {
		return nil, false
	}
	return entry.rgba, true
}

func (cache *effectLayerCache) putOpacity(begin, end int, scale float64, bounds image.Rectangle, opacity uint8, canvas *image.RGBA) {
	if canvas == nil || canvas.Bounds() != bounds || bounds.Empty() {
		return
	}
	cache.put(opacityEffectLayerKey(begin, end, scale, bounds, opacity), &effectLayerCacheEntry{
		rgba: canvas, bytes: len(canvas.Pix),
	})
}

func (cache *effectLayerCache) getTransform(begin, end int, scale float64, bounds, output image.Rectangle, transform AffineTransform) (*image.RGBA64, bool) {
	entry, ok := cache.get(transformEffectLayerKey(begin, end, scale, bounds, output, transform))
	if !ok || entry == nil || entry.rgba64 == nil {
		return nil, false
	}
	return entry.rgba64, true
}

func (cache *effectLayerCache) putTransform(begin, end int, scale float64, bounds, output image.Rectangle, transform AffineTransform, samples *image.RGBA64) {
	if samples == nil || samples.Bounds() != output || output.Empty() {
		return
	}
	cache.put(transformEffectLayerKey(begin, end, scale, bounds, output, transform), &effectLayerCacheEntry{
		rgba64: samples, bytes: len(samples.Pix),
	})
}

func (cache *effectLayerCache) get(key effectLayerCacheKey) (*effectLayerCacheEntry, bool) {
	if cache == nil {
		return nil, false
	}
	cache.mutex.RLock()
	entry, ok := cache.entries[key]
	cache.mutex.RUnlock()
	if !ok {
		return nil, false
	}
	entry.lastUsed.Store(cache.clock.Add(1))
	return entry, true
}

func (cache *effectLayerCache) put(key effectLayerCacheKey, entry *effectLayerCacheEntry) {
	if cache == nil || entry == nil || entry.bytes <= 0 || entry.bytes > maxEffectLayerCacheBytes {
		return
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if cache.entries == nil {
		cache.entries = make(map[effectLayerCacheKey]*effectLayerCacheEntry)
	}
	if previous, ok := cache.entries[key]; ok {
		cache.bytes -= previous.bytes
		delete(cache.entries, key)
	}
	for cache.bytes+entry.bytes > maxEffectLayerCacheBytes && len(cache.entries) != 0 {
		var oldestKey effectLayerCacheKey
		oldestUse := ^uint64(0)
		for candidateKey, candidate := range cache.entries {
			if used := candidate.lastUsed.Load(); used < oldestUse {
				oldestKey, oldestUse = candidateKey, used
			}
		}
		oldest := cache.entries[oldestKey]
		delete(cache.entries, oldestKey)
		cache.bytes -= oldest.bytes
	}
	entry.lastUsed.Store(cache.clock.Add(1))
	cache.entries[key] = entry
	cache.bytes += entry.bytes
}

func (cache *effectLayerCache) clear() {
	if cache == nil {
		return
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.entries = nil
	cache.bytes = 0
	cache.clock.Store(0)
}

func matchingOpacityEnd(commands []Command, begin int) (int, bool) {
	return matchingGroupEnd(commands, begin, EndOpacityCommand)
}

func matchingTransformEnd(commands []Command, begin int) (int, bool) {
	return matchingGroupEnd(commands, begin, EndTransformCommand)
}

func matchingClipEnd(commands []Command, begin int) (int, bool) {
	return matchingGroupEnd(commands, begin, EndClipCommand)
}

func matchingGroupEnd(commands []Command, begin int, targetEnd CommandKind) (int, bool) {
	var stack [16]CommandKind
	depth := 0
	for index := begin + 1; index < len(commands); index++ {
		kind := commands[index].Kind
		switch kind {
		case BeginOpacityCommand:
			if depth >= len(stack) {
				return 0, false
			}
			stack[depth] = EndOpacityCommand
			depth++
		case BeginTransformCommand:
			if depth >= len(stack) {
				return 0, false
			}
			stack[depth] = EndTransformCommand
			depth++
		case BeginClipCommand:
			if depth >= len(stack) {
				return 0, false
			}
			stack[depth] = EndClipCommand
			depth++
		case EndOpacityCommand, EndTransformCommand, EndClipCommand:
			if depth == 0 {
				if kind == targetEnd {
					return index, true
				}
				return 0, false
			}
			if stack[depth-1] != kind {
				return 0, false
			}
			depth--
		case DrawTextCommand:
			if commands[index].textOrientation != textPaintHorizontal {
				return 0, false
			}
		}
	}
	return 0, false
}
