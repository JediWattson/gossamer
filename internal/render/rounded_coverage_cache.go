package render

import (
	"image"
	"image/color"
	"math"
	"sync"
)

const maxRoundedCoverageCacheBytes = 8 * 1024 * 1024

type roundedCoverageKey struct {
	rectangle       [4]uint64
	radii           [8]uint64
	paintBounds     image.Rectangle
	shapeModeBounds image.Rectangle
	pathTarget      image.Rectangle
}

type roundedCoverageEntry struct {
	mask     *image.Alpha16
	lastUsed uint64
}

// roundedCoverageCache retains vector's 16-bit coverage, not post-blended
// pixels. Applying that coverage to the current framebuffer preserves the
// original one-stage Over rounding while avoiding repeated path rasterization.
// FrameRasterizer's mutex serializes access to this small session-owned cache.
type roundedCoverageCache struct {
	mutex   sync.RWMutex
	entries map[roundedCoverageKey]roundedCoverageEntry
	bytes   int
	clock   uint64
}

func (cache *roundedCoverageCache) paint(target *image.RGBA, rectangle Rect, radii CornerRadii, fill color.NRGBA, shapeModeBounds image.Rectangle) bool {
	paintBounds := vectorPaintBounds(target.Bounds(), rectangle)
	stableBounds := vectorPaintBounds(shapeModeBounds, rectangle)
	if paintBounds.Empty() || fill.A == 0 {
		return true
	}
	maskBounds := paintBounds
	pathTarget := target.Bounds()
	if !stableBounds.Empty() && paintBounds.Min != stableBounds.Min && paintBounds.In(stableBounds) {
		maskBounds = stableBounds
		pathTarget = stableBounds
	}
	key, ok := newRoundedCoverageKey(rectangle, radii, maskBounds, shapeModeBounds, pathTarget)
	if !ok {
		return false
	}
	entryBytes := maskBounds.Dx() * maskBounds.Dy() * 2
	if entryBytes <= 0 || entryBytes > maxRoundedCoverageCacheBytes {
		return false
	}
	cache.mutex.RLock()
	entry, ok := cache.entries[key]
	cache.mutex.RUnlock()
	if !ok {
		cache.mutex.Lock()
		entry, ok = cache.entries[key]
		if !ok {
			if cache.entries == nil {
				cache.entries = make(map[roundedCoverageKey]roundedCoverageEntry)
			}
			mask := image.NewAlpha16(maskBounds)
			rasterizer := roundedRectRasterizer(rectangle, radii, maskBounds, shapeModeBounds, pathTarget)
			rasterizer.Draw(mask, maskBounds, image.NewUniform(color.Alpha16{A: 0xffff}), image.Point{})
			cache.clock++
			entry = roundedCoverageEntry{mask: mask, lastUsed: cache.clock}
			cache.makeRoom(entryBytes)
			cache.bytes += entryBytes
			cache.entries[key] = entry
		}
		cache.mutex.Unlock()
	}
	drawRGBAUniformCoverageOver(target, paintBounds, fill, entry.mask)
	return true
}

func (cache *roundedCoverageCache) makeRoom(incoming int) {
	for cache.bytes+incoming > maxRoundedCoverageCacheBytes && len(cache.entries) > 0 {
		var oldestKey roundedCoverageKey
		oldestUse := ^uint64(0)
		for key, entry := range cache.entries {
			if entry.lastUsed < oldestUse {
				oldestKey, oldestUse = key, entry.lastUsed
			}
		}
		entry := cache.entries[oldestKey]
		cache.bytes -= len(entry.mask.Pix)
		delete(cache.entries, oldestKey)
	}
}

func (cache *roundedCoverageCache) clear() {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.entries = nil
	cache.bytes = 0
	cache.clock = 0
}

func newRoundedCoverageKey(rectangle Rect, radii CornerRadii, paintBounds, shapeModeBounds, pathTarget image.Rectangle) (roundedCoverageKey, bool) {
	values := [...]float64{
		rectangle.X, rectangle.Y, rectangle.Width, rectangle.Height,
		radii.TopLeft.X, radii.TopLeft.Y, radii.TopRight.X, radii.TopRight.Y,
		radii.BottomRight.X, radii.BottomRight.Y, radii.BottomLeft.X, radii.BottomLeft.Y,
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return roundedCoverageKey{}, false
		}
	}
	key := roundedCoverageKey{
		paintBounds: paintBounds, shapeModeBounds: shapeModeBounds, pathTarget: pathTarget,
	}
	for index := range key.rectangle {
		key.rectangle[index] = math.Float64bits(values[index])
	}
	for index := range key.radii {
		key.radii[index] = math.Float64bits(values[index+len(key.rectangle)])
	}
	return key, true
}

// drawRGBAUniformCoverageOver matches x/image/vector's concrete RGBA uniform
// Over formula. Alpha16 retains the full coverage value used by vector; Alpha8
// or a pre-flattened RGBA overlay would introduce another rounding step.
func drawRGBAUniformCoverageOver(destination *image.RGBA, rectangle image.Rectangle, fill color.NRGBA, mask *image.Alpha16) {
	sr, sg, sb, sa := fill.RGBA()
	for y := rectangle.Min.Y; y < rectangle.Max.Y; y++ {
		destinationOffset := destination.PixOffset(rectangle.Min.X, y)
		maskOffset := mask.PixOffset(rectangle.Min.X, y)
		for x := 0; x < rectangle.Dx(); x++ {
			ma := uint32(mask.Pix[maskOffset])<<8 | uint32(mask.Pix[maskOffset+1])
			if ma != 0 {
				a := uint32(0xffff) - sa*ma/0xffff
				pixel := destination.Pix[destinationOffset : destinationOffset+4 : destinationOffset+4]
				pixel[0] = uint8(((uint32(pixel[0])*0x101*a + sr*ma) / 0xffff) >> 8)
				pixel[1] = uint8(((uint32(pixel[1])*0x101*a + sg*ma) / 0xffff) >> 8)
				pixel[2] = uint8(((uint32(pixel[2])*0x101*a + sb*ma) / 0xffff) >> 8)
				pixel[3] = uint8(((uint32(pixel[3])*0x101*a + sa*ma) / 0xffff) >> 8)
			}
			destinationOffset += 4
			maskOffset += 2
		}
	}
}
