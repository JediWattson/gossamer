package nativeengine

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/style"
	"golang.org/x/image/vector"
)

const (
	nativeCanvasWidthGet uint64 = 29_000 + iota
	nativeCanvasWidthSet
	nativeCanvasHeightGet
	nativeCanvasHeightSet
	nativeCanvasGetContext
	nativeCanvasToDataURL
	nativeImageDataConstructor

	nativeCanvas2DCanvasGet
	nativeCanvas2DFillStyleGet
	nativeCanvas2DFillStyleSet
	nativeCanvas2DStrokeStyleGet
	nativeCanvas2DStrokeStyleSet
	nativeCanvas2DLineWidthGet
	nativeCanvas2DLineWidthSet
	nativeCanvas2DLineCapGet
	nativeCanvas2DLineCapSet
	nativeCanvas2DLineJoinGet
	nativeCanvas2DLineJoinSet
	nativeCanvas2DMiterLimitGet
	nativeCanvas2DMiterLimitSet
	nativeCanvas2DGlobalAlphaGet
	nativeCanvas2DGlobalAlphaSet
	nativeCanvas2DGlobalCompositeGet
	nativeCanvas2DGlobalCompositeSet
	nativeCanvas2DFontGet
	nativeCanvas2DFontSet
	nativeCanvas2DTextAlignGet
	nativeCanvas2DTextAlignSet
	nativeCanvas2DTextBaselineGet
	nativeCanvas2DTextBaselineSet

	nativeCanvas2DSave
	nativeCanvas2DRestore
	nativeCanvas2DReset
	nativeCanvas2DScale
	nativeCanvas2DRotate
	nativeCanvas2DTranslate
	nativeCanvas2DTransform
	nativeCanvas2DSetTransform
	nativeCanvas2DResetTransform
	nativeCanvas2DClearRect
	nativeCanvas2DFillRect
	nativeCanvas2DStrokeRect
	nativeCanvas2DBeginPath
	nativeCanvas2DClosePath
	nativeCanvas2DMoveTo
	nativeCanvas2DLineTo
	nativeCanvas2DRect
	nativeCanvas2DRoundRect
	nativeCanvas2DArc
	nativeCanvas2DArcTo
	nativeCanvas2DEllipse
	nativeCanvas2DQuadraticCurveTo
	nativeCanvas2DBezierCurveTo
	nativeCanvas2DFill
	nativeCanvas2DStroke
	nativeCanvas2DCreateImageData
	nativeCanvas2DGetImageData
	nativeCanvas2DPutImageData
	nativeCanvas2DDrawImage
	nativeCanvas2DFillText
	nativeCanvas2DStrokeText
	nativeCanvas2DMeasureText
)

func (realm *Realm) canvasNativeRegistrations() []nativeRegistration {
	return []nativeRegistration{
		{nativeCanvasWidthGet, realm.canvasDimensionGet(false)},
		{nativeCanvasWidthSet, realm.canvasDimensionSet("width")},
		{nativeCanvasHeightGet, realm.canvasDimensionGet(true)},
		{nativeCanvasHeightSet, realm.canvasDimensionSet("height")},
		{nativeCanvasGetContext, realm.canvasGetContext},
		{nativeCanvasToDataURL, realm.canvasToDataURL},
		{nativeImageDataConstructor, realm.imageDataConstructor},

		{nativeCanvas2DCanvasGet, realm.canvas2DCanvasGet},
		{nativeCanvas2DFillStyleGet, realm.canvas2DFillStyleGet},
		{nativeCanvas2DFillStyleSet, realm.canvas2DFillStyleSet},
		{nativeCanvas2DStrokeStyleGet, realm.canvas2DStrokeStyleGet},
		{nativeCanvas2DStrokeStyleSet, realm.canvas2DStrokeStyleSet},
		{nativeCanvas2DLineWidthGet, realm.canvas2DLineWidthGet},
		{nativeCanvas2DLineWidthSet, realm.canvas2DLineWidthSet},
		{nativeCanvas2DLineCapGet, realm.canvas2DLineCapGet},
		{nativeCanvas2DLineCapSet, realm.canvas2DLineCapSet},
		{nativeCanvas2DLineJoinGet, realm.canvas2DLineJoinGet},
		{nativeCanvas2DLineJoinSet, realm.canvas2DLineJoinSet},
		{nativeCanvas2DMiterLimitGet, realm.canvas2DMiterLimitGet},
		{nativeCanvas2DMiterLimitSet, realm.canvas2DMiterLimitSet},
		{nativeCanvas2DGlobalAlphaGet, realm.canvas2DGlobalAlphaGet},
		{nativeCanvas2DGlobalAlphaSet, realm.canvas2DGlobalAlphaSet},
		{nativeCanvas2DGlobalCompositeGet, realm.canvas2DGlobalCompositeGet},
		{nativeCanvas2DGlobalCompositeSet, realm.canvas2DGlobalCompositeSet},
		{nativeCanvas2DFontGet, realm.canvas2DFontGet},
		{nativeCanvas2DFontSet, realm.canvas2DFontSet},
		{nativeCanvas2DTextAlignGet, realm.canvas2DTextAlignGet},
		{nativeCanvas2DTextAlignSet, realm.canvas2DTextAlignSet},
		{nativeCanvas2DTextBaselineGet, realm.canvas2DTextBaselineGet},
		{nativeCanvas2DTextBaselineSet, realm.canvas2DTextBaselineSet},

		{nativeCanvas2DSave, realm.canvas2DSave},
		{nativeCanvas2DRestore, realm.canvas2DRestore},
		{nativeCanvas2DReset, realm.canvas2DReset},
		{nativeCanvas2DScale, realm.canvas2DScale},
		{nativeCanvas2DRotate, realm.canvas2DRotate},
		{nativeCanvas2DTranslate, realm.canvas2DTranslate},
		{nativeCanvas2DTransform, realm.canvas2DTransform},
		{nativeCanvas2DSetTransform, realm.canvas2DSetTransform},
		{nativeCanvas2DResetTransform, realm.canvas2DResetTransform},
		{nativeCanvas2DClearRect, realm.canvas2DClearRect},
		{nativeCanvas2DFillRect, realm.canvas2DFillRect},
		{nativeCanvas2DStrokeRect, realm.canvas2DStrokeRect},
		{nativeCanvas2DBeginPath, realm.canvas2DBeginPath},
		{nativeCanvas2DClosePath, realm.canvas2DClosePath},
		{nativeCanvas2DMoveTo, realm.canvas2DMoveTo},
		{nativeCanvas2DLineTo, realm.canvas2DLineTo},
		{nativeCanvas2DRect, realm.canvas2DRect},
		{nativeCanvas2DRoundRect, realm.canvas2DRoundRect},
		{nativeCanvas2DArc, realm.canvas2DArc},
		{nativeCanvas2DArcTo, realm.canvas2DArcTo},
		{nativeCanvas2DEllipse, realm.canvas2DEllipse},
		{nativeCanvas2DQuadraticCurveTo, realm.canvas2DQuadraticCurveTo},
		{nativeCanvas2DBezierCurveTo, realm.canvas2DBezierCurveTo},
		{nativeCanvas2DFill, realm.canvas2DFill},
		{nativeCanvas2DStroke, realm.canvas2DStroke},
		{nativeCanvas2DCreateImageData, realm.canvas2DCreateImageData},
		{nativeCanvas2DGetImageData, realm.canvas2DGetImageData},
		{nativeCanvas2DPutImageData, realm.canvas2DPutImageData},
		{nativeCanvas2DDrawImage, realm.canvas2DDrawImage},
		{nativeCanvas2DFillText, realm.canvas2DFillText},
		{nativeCanvas2DStrokeText, realm.canvas2DStrokeText},
		{nativeCanvas2DMeasureText, realm.canvas2DMeasureText},
	}
}

func (realm *Realm) installCanvasPrototypeProperties(context *browserruntime.TaskContext) error {
	methods := []struct {
		target memory.Ref
		name   string
		arity  uint32
		id     uint64
	}{
		{realm.bindings.htmlCanvasElementPrototype, "getContext", 1, nativeCanvasGetContext},
		{realm.bindings.htmlCanvasElementPrototype, "toDataURL", 0, nativeCanvasToDataURL},
		{realm.bindings.canvas2DPrototype, "save", 0, nativeCanvas2DSave},
		{realm.bindings.canvas2DPrototype, "restore", 0, nativeCanvas2DRestore},
		{realm.bindings.canvas2DPrototype, "reset", 0, nativeCanvas2DReset},
		{realm.bindings.canvas2DPrototype, "scale", 2, nativeCanvas2DScale},
		{realm.bindings.canvas2DPrototype, "rotate", 1, nativeCanvas2DRotate},
		{realm.bindings.canvas2DPrototype, "translate", 2, nativeCanvas2DTranslate},
		{realm.bindings.canvas2DPrototype, "transform", 6, nativeCanvas2DTransform},
		{realm.bindings.canvas2DPrototype, "setTransform", 6, nativeCanvas2DSetTransform},
		{realm.bindings.canvas2DPrototype, "resetTransform", 0, nativeCanvas2DResetTransform},
		{realm.bindings.canvas2DPrototype, "clearRect", 4, nativeCanvas2DClearRect},
		{realm.bindings.canvas2DPrototype, "fillRect", 4, nativeCanvas2DFillRect},
		{realm.bindings.canvas2DPrototype, "strokeRect", 4, nativeCanvas2DStrokeRect},
		{realm.bindings.canvas2DPrototype, "beginPath", 0, nativeCanvas2DBeginPath},
		{realm.bindings.canvas2DPrototype, "closePath", 0, nativeCanvas2DClosePath},
		{realm.bindings.canvas2DPrototype, "moveTo", 2, nativeCanvas2DMoveTo},
		{realm.bindings.canvas2DPrototype, "lineTo", 2, nativeCanvas2DLineTo},
		{realm.bindings.canvas2DPrototype, "rect", 4, nativeCanvas2DRect},
		{realm.bindings.canvas2DPrototype, "roundRect", 5, nativeCanvas2DRoundRect},
		{realm.bindings.canvas2DPrototype, "arc", 5, nativeCanvas2DArc},
		{realm.bindings.canvas2DPrototype, "arcTo", 5, nativeCanvas2DArcTo},
		{realm.bindings.canvas2DPrototype, "ellipse", 7, nativeCanvas2DEllipse},
		{realm.bindings.canvas2DPrototype, "quadraticCurveTo", 4, nativeCanvas2DQuadraticCurveTo},
		{realm.bindings.canvas2DPrototype, "bezierCurveTo", 6, nativeCanvas2DBezierCurveTo},
		{realm.bindings.canvas2DPrototype, "fill", 0, nativeCanvas2DFill},
		{realm.bindings.canvas2DPrototype, "stroke", 0, nativeCanvas2DStroke},
		{realm.bindings.canvas2DPrototype, "createImageData", 2, nativeCanvas2DCreateImageData},
		{realm.bindings.canvas2DPrototype, "getImageData", 4, nativeCanvas2DGetImageData},
		{realm.bindings.canvas2DPrototype, "putImageData", 3, nativeCanvas2DPutImageData},
		{realm.bindings.canvas2DPrototype, "drawImage", 3, nativeCanvas2DDrawImage},
		{realm.bindings.canvas2DPrototype, "fillText", 3, nativeCanvas2DFillText},
		{realm.bindings.canvas2DPrototype, "strokeText", 3, nativeCanvas2DStrokeText},
		{realm.bindings.canvas2DPrototype, "measureText", 1, nativeCanvas2DMeasureText},
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
		if err := defineData(context, method.target, method.name, memory.RefValue(fn), true, false, true); err != nil {
			return err
		}
	}

	accessors := []struct {
		target memory.Ref
		name   string
		getter uint64
		setter uint64
	}{
		{realm.bindings.htmlCanvasElementPrototype, "width", nativeCanvasWidthGet, nativeCanvasWidthSet},
		{realm.bindings.htmlCanvasElementPrototype, "height", nativeCanvasHeightGet, nativeCanvasHeightSet},
		{realm.bindings.canvas2DPrototype, "canvas", nativeCanvas2DCanvasGet, 0},
		{realm.bindings.canvas2DPrototype, "fillStyle", nativeCanvas2DFillStyleGet, nativeCanvas2DFillStyleSet},
		{realm.bindings.canvas2DPrototype, "strokeStyle", nativeCanvas2DStrokeStyleGet, nativeCanvas2DStrokeStyleSet},
		{realm.bindings.canvas2DPrototype, "lineWidth", nativeCanvas2DLineWidthGet, nativeCanvas2DLineWidthSet},
		{realm.bindings.canvas2DPrototype, "lineCap", nativeCanvas2DLineCapGet, nativeCanvas2DLineCapSet},
		{realm.bindings.canvas2DPrototype, "lineJoin", nativeCanvas2DLineJoinGet, nativeCanvas2DLineJoinSet},
		{realm.bindings.canvas2DPrototype, "miterLimit", nativeCanvas2DMiterLimitGet, nativeCanvas2DMiterLimitSet},
		{realm.bindings.canvas2DPrototype, "globalAlpha", nativeCanvas2DGlobalAlphaGet, nativeCanvas2DGlobalAlphaSet},
		{realm.bindings.canvas2DPrototype, "globalCompositeOperation", nativeCanvas2DGlobalCompositeGet, nativeCanvas2DGlobalCompositeSet},
		{realm.bindings.canvas2DPrototype, "font", nativeCanvas2DFontGet, nativeCanvas2DFontSet},
		{realm.bindings.canvas2DPrototype, "textAlign", nativeCanvas2DTextAlignGet, nativeCanvas2DTextAlignSet},
		{realm.bindings.canvas2DPrototype, "textBaseline", nativeCanvas2DTextBaselineGet, nativeCanvas2DTextBaselineSet},
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
		if err := defineAccessor(context, accessor.target, accessor.name, memory.RefValue(getter), setter); err != nil {
			return err
		}
	}
	return nil
}

type canvasPoint struct {
	x float64
	y float64
}

type canvasSubpath struct {
	points []canvasPoint
	closed bool
}

type canvasMatrix [6]float64

var canvasIdentityMatrix = canvasMatrix{1, 0, 0, 1, 0, 0}

func (m canvasMatrix) transformPoint(x, y float64) canvasPoint {
	return canvasPoint{
		x: m[0]*x + m[2]*y + m[4],
		y: m[1]*x + m[3]*y + m[5],
	}
}

func (m canvasMatrix) multiply(other canvasMatrix) canvasMatrix {
	return canvasMatrix{
		m[0]*other[0] + m[2]*other[1],
		m[1]*other[0] + m[3]*other[1],
		m[0]*other[2] + m[2]*other[3],
		m[1]*other[2] + m[3]*other[3],
		m[0]*other[4] + m[2]*other[5] + m[4],
		m[1]*other[4] + m[3]*other[5] + m[5],
	}
}

func (m canvasMatrix) effectiveScale() float64 {
	det := math.Abs(m[0]*m[3] - m[1]*m[2])
	if det == 0 || !isFiniteFloat(det) {
		return 1
	}
	return math.Sqrt(det)
}

func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

type canvas2DDrawState struct {
	fillStyle                string
	fillColor                color.NRGBA
	strokeStyle              string
	strokeColor              color.NRGBA
	lineWidth                float64
	lineCap                  string
	lineJoin                 string
	miterLimit               float64
	globalAlpha              float64
	globalCompositeOperation string
	font                     string
	textAlign                string
	textBaseline             string
	transform                canvasMatrix
}

func defaultCanvas2DDrawState() canvas2DDrawState {
	black := color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	return canvas2DDrawState{
		fillStyle:                "#000000",
		fillColor:                black,
		strokeStyle:              "#000000",
		strokeColor:              black,
		lineWidth:                1,
		lineCap:                  "butt",
		lineJoin:                 "miter",
		miterLimit:               10,
		globalAlpha:              1,
		globalCompositeOperation: "source-over",
		font:                     "10px sans-serif",
		textAlign:                "start",
		textBaseline:             "alphabetic",
		transform:                canvasIdentityMatrix,
	}
}

type canvas2DState struct {
	handle   browser.NodeHandle
	revision uint64
	current  canvas2DDrawState
	stack    []canvas2DDrawState
	subpaths []canvasSubpath
}

func newCanvas2DState(handle browser.NodeHandle, revision uint64) *canvas2DState {
	state := &canvas2DState{handle: handle}
	state.reset(revision)
	return state
}

func (state *canvas2DState) reset(revision uint64) {
	state.revision = revision
	state.current = defaultCanvas2DDrawState()
	state.stack = nil
	state.subpaths = nil
}

func (state *canvas2DState) moveTo(x, y float64) {
	if !isFiniteFloat(x) || !isFiniteFloat(y) {
		return
	}
	pt := state.current.transform.transformPoint(x, y)
	state.subpaths = append(state.subpaths, canvasSubpath{
		points: []canvasPoint{pt},
	})
}

func (state *canvas2DState) ensureSubpath(x, y float64) *canvasSubpath {
	if len(state.subpaths) == 0 {
		state.moveTo(x, y)
	}
	return &state.subpaths[len(state.subpaths)-1]
}

func (state *canvas2DState) lineTo(x, y float64) {
	if !isFiniteFloat(x) || !isFiniteFloat(y) {
		return
	}
	pt := state.current.transform.transformPoint(x, y)
	if len(state.subpaths) == 0 {
		state.subpaths = append(state.subpaths, canvasSubpath{points: []canvasPoint{pt}})
		return
	}
	sub := &state.subpaths[len(state.subpaths)-1]
	sub.points = append(sub.points, pt)
}

func (state *canvas2DState) closePath() {
	if len(state.subpaths) == 0 {
		return
	}
	sub := &state.subpaths[len(state.subpaths)-1]
	if len(sub.points) == 0 {
		return
	}
	sub.closed = true
	first := sub.points[0]
	state.subpaths = append(state.subpaths, canvasSubpath{
		points: []canvasPoint{first},
	})
}

func (state *canvas2DState) rect(x, y, w, h float64) {
	if !isFiniteFloat(x) || !isFiniteFloat(y) || !isFiniteFloat(w) || !isFiniteFloat(h) {
		return
	}
	p0 := state.current.transform.transformPoint(x, y)
	p1 := state.current.transform.transformPoint(x+w, y)
	p2 := state.current.transform.transformPoint(x+w, y+h)
	p3 := state.current.transform.transformPoint(x, y+h)
	state.subpaths = append(state.subpaths, canvasSubpath{
		points: []canvasPoint{p0, p1, p2, p3},
		closed: true,
	})
	state.subpaths = append(state.subpaths, canvasSubpath{
		points: []canvasPoint{p0},
	})
}

func (realm *Realm) canvasHost(context *browserruntime.TaskContext, handle browser.NodeHandle) (browser.DOMCanvasHost, error) {
	host, ok := realm.host.(browser.DOMCanvasHost)
	if !ok {
		return nil, realm.throwDOMException(context, fmt.Errorf("nativeengine: browser host does not support canvas"))
	}
	return host, nil
}

func (realm *Realm) unwrapCanvasHandle(context *browserruntime.TaskContext, this memory.Value) (browser.NodeHandle, browser.DOMCanvasHost, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return browser.NodeHandle{}, nil, err
	}
	host, err := realm.canvasHost(context, handle)
	if err != nil {
		return browser.NodeHandle{}, nil, err
	}
	if _, _, _, err := host.CanvasSize(handle); err != nil {
		return browser.NodeHandle{}, nil, realm.throwDOMException(context, err)
	}
	return handle, host, nil
}

func (realm *Realm) canvasDimensionGet(vertical bool) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
		handle, host, err := realm.unwrapCanvasHandle(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		width, height, _, err := host.CanvasSize(handle)
		if err != nil {
			return memory.Value{}, realm.throwDOMException(context, err)
		}
		if vertical {
			return memory.NumberValue(float64(height)), nil
		}
		return memory.NumberValue(float64(width)), nil
	}
}

func (realm *Realm) canvasDimensionSet(attr string) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
		handle, host, err := realm.unwrapCanvasHandle(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		num, err := canvasNumber(context, argument(arguments, 0))
		if err != nil {
			return memory.Value{}, err
		}
		var u uint32
		if isFiniteFloat(num) {
			u = uint32(int64(math.Trunc(num)))
		}
		// Match WebIDL unsigned long + HTML canvas fallback (> MaxInt32 falls back to default).
		var rawValue string
		if u > math.MaxInt32 {
			fallback := dom.DefaultCanvasWidth
			if attr == "height" {
				fallback = dom.DefaultCanvasHeight
			}
			rawValue = strconv.Itoa(fallback)
		} else {
			rawValue = strconv.FormatUint(uint64(u), 10)
		}
		_, _, rev, err := host.SetCanvasDimension(handle, attr, rawValue)
		if err != nil {
			return memory.Value{}, realm.throwDOMException(context, err)
		}
		if realm.canvas2DStates != nil {
			if state, exists := realm.canvas2DStates[handle]; exists {
				state.reset(rev)
			}
		}
		return memory.UndefinedValue(), nil
	}
}

func (realm *Realm) canvasGetContext(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, host, err := realm.unwrapCanvasHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	contextType, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if contextType != "2d" {
		return memory.NullValue(), nil
	}
	_, _, rev, err := host.CanvasSize(handle)
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	if realm.canvas2DStates == nil {
		realm.canvas2DStates = make(map[browser.NodeHandle]*canvas2DState)
	}
	if _, exists := realm.canvas2DStates[handle]; !exists {
		realm.canvas2DStates[handle] = newCanvas2DState(handle, rev)
	}
	return realm.elementFacade(context, this, hostClassCanvas2D, realm.bindings.canvas2DPrototype, "ctx2d")
}

func (realm *Realm) canvasToDataURL(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, host, err := realm.unwrapCanvasHandle(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	mimeType := "image/png"
	if arg0 := argument(arguments, 0); arg0.Kind() != memory.ValueUndefined {
		requested, strErr := valueString(context, arg0)
		if strErr != nil {
			return memory.Value{}, strErr
		}
		lower := strings.ToLower(strings.TrimSpace(requested))
		if lower == "image/jpeg" || lower == "image/png" {
			mimeType = lower
		}
	}
	quality := 92
	if arg1 := argument(arguments, 1); arg1.Kind() == memory.ValueNumber {
		q := arg1.Number()
		if isFiniteFloat(q) && q >= 0 && q <= 1 {
			quality = int(math.Round(q * 100))
			if quality < 1 {
				quality = 1
			}
		}
	}

	var encoded string
	var encodeErr error
	err = host.ReadCanvas(handle, func(width, height int, _ uint64, bitmap *image.RGBA) {
		if width <= 0 || height <= 0 {
			encoded = "data:,"
			return
		}
		src := bitmap
		if src == nil {
			src = image.NewRGBA(image.Rect(0, 0, width, height))
		}
		var buf bytes.Buffer
		if mimeType == "image/jpeg" {
			encodeErr = jpeg.Encode(&buf, src, &jpeg.Options{Quality: quality})
		} else {
			encodeErr = png.Encode(&buf, src)
		}
		if encodeErr != nil {
			return
		}
		encoded = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	})
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	if encodeErr != nil {
		return memory.Value{}, realm.throwDOMException(context, encodeErr)
	}
	return newString(context, encoded)
}

func (realm *Realm) requireCanvas2D(context *browserruntime.TaskContext, this memory.Value) (*canvas2DState, browser.DOMCanvasHost, error) {
	if !this.IsRef() {
		return nil, nil, fmt.Errorf("%w: incompatible CanvasRenderingContext2D receiver", browserruntime.ErrOperandType)
	}
	record, ok, err := realm.facadeRecord(context, this.Ref())
	if err != nil {
		return nil, nil, err
	}
	if !ok || record.Class != hostClassCanvas2D {
		return nil, nil, fmt.Errorf("%w: incompatible CanvasRenderingContext2D receiver", browserruntime.ErrOperandType)
	}
	handle := browser.NodeHandle{
		Document: browser.DocumentGeneration(record.Scope),
		Node:     dom.NodeID(record.Identity),
	}
	host, err := realm.canvasHost(context, handle)
	if err != nil {
		return nil, nil, err
	}
	_, _, rev, err := host.CanvasSize(handle)
	if err != nil {
		return nil, nil, realm.throwDOMException(context, err)
	}
	if realm.canvas2DStates == nil {
		realm.canvas2DStates = make(map[browser.NodeHandle]*canvas2DState)
	}
	state, exists := realm.canvas2DStates[handle]
	if !exists {
		state = newCanvas2DState(handle, rev)
		realm.canvas2DStates[handle] = state
	} else if rev != state.revision {
		// External DOM attribute mutation on width/height reset the bitmap and context.
		state.reset(rev)
	}
	return state, host, nil
}

func (realm *Realm) canvas2DCanvasGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.wrappedNodeValue(context, state.handle)
}

func parseCanvasCSSColor(raw string) (color.NRGBA, string, bool) {
	parsed, ok := style.ParseColor(strings.TrimSpace(raw), color.NRGBA{A: 255})
	if !ok {
		return color.NRGBA{}, "", false
	}
	return parsed, serializeCanvasColor(parsed), true
}

func serializeCanvasColor(c color.NRGBA) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	alpha := float64(c.A) / 255.0
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", c.R, c.G, c.B, strconv.FormatFloat(alpha, 'f', -1, 64))
}

func (realm *Realm) canvas2DFillStyleGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, state.current.fillStyle)
}

func (realm *Realm) canvas2DFillStyleSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	val := argument(arguments, 0)
	if isStringValue(context, val) || val.Kind() == memory.ValueNumber {
		raw, strErr := valueString(context, val)
		if strErr != nil {
			return memory.Value{}, strErr
		}
		if col, serialized, ok := parseCanvasCSSColor(raw); ok {
			state.current.fillColor = col
			state.current.fillStyle = serialized
		}
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DStrokeStyleGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, state.current.strokeStyle)
}

func (realm *Realm) canvas2DStrokeStyleSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	val := argument(arguments, 0)
	if isStringValue(context, val) || val.Kind() == memory.ValueNumber {
		raw, strErr := valueString(context, val)
		if strErr != nil {
			return memory.Value{}, strErr
		}
		if col, serialized, ok := parseCanvasCSSColor(raw); ok {
			state.current.strokeColor = col
			state.current.strokeStyle = serialized
		}
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DLineWidthGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(state.current.lineWidth), nil
}

func (realm *Realm) canvas2DLineWidthSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	n, err := canvasNumber(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if isFiniteFloat(n) && n > 0 {
		state.current.lineWidth = n
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DLineCapGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, state.current.lineCap)
}

func (realm *Realm) canvas2DLineCapSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	raw, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	switch raw {
	case "butt", "round", "square":
		state.current.lineCap = raw
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DLineJoinGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, state.current.lineJoin)
}

func (realm *Realm) canvas2DLineJoinSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	raw, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	switch raw {
	case "miter", "round", "bevel":
		state.current.lineJoin = raw
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DMiterLimitGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(state.current.miterLimit), nil
}

func (realm *Realm) canvas2DMiterLimitSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	n, err := canvasNumber(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if isFiniteFloat(n) && n > 0 {
		state.current.miterLimit = n
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DGlobalAlphaGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(state.current.globalAlpha), nil
}

func (realm *Realm) canvas2DGlobalAlphaSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	n, err := canvasNumber(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if isFiniteFloat(n) && n >= 0 && n <= 1 {
		state.current.globalAlpha = n
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DGlobalCompositeGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, state.current.globalCompositeOperation)
}

func (realm *Realm) canvas2DGlobalCompositeSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	raw, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	switch raw {
	case "source-over", "copy", "destination-out", "source-in", "source-atop", "lighter":
		state.current.globalCompositeOperation = raw
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DFontGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, state.current.font)
}

func (realm *Realm) canvas2DFontSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	raw, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if strings.TrimSpace(raw) != "" {
		state.current.font = strings.TrimSpace(raw)
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DTextAlignGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, state.current.textAlign)
}

func (realm *Realm) canvas2DTextAlignSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	raw, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	switch raw {
	case "start", "end", "left", "right", "center":
		state.current.textAlign = raw
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DTextBaselineGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, state.current.textBaseline)
}

func (realm *Realm) canvas2DTextBaselineSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	raw, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	switch raw {
	case "top", "hanging", "middle", "alphabetic", "ideographic", "bottom":
		state.current.textBaseline = raw
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DSave(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	state.stack = append(state.stack, state.current)
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DRestore(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if n := len(state.stack); n > 0 {
		state.current = state.stack[n-1]
		state.stack = state.stack[:n-1]
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DReset(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, host, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	rev, err := host.MutateCanvas(state.handle, func(_, _ int, bitmap *image.RGBA) bool {
		if bitmap == nil {
			return false
		}
		clear(bitmap.Pix)
		return true
	})
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	state.reset(rev)
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DScale(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	x, y, err := canvasArgs2(context, arguments)
	if err != nil || !isFiniteFloat(x) || !isFiniteFloat(y) {
		return memory.UndefinedValue(), err
	}
	state.current.transform = state.current.transform.multiply(canvasMatrix{x, 0, 0, y, 0, 0})
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DRotate(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	angle, err := canvasNumber(context, argument(arguments, 0))
	if err != nil || !isFiniteFloat(angle) {
		return memory.UndefinedValue(), err
	}
	s, c := math.Sincos(angle)
	state.current.transform = state.current.transform.multiply(canvasMatrix{c, s, -s, c, 0, 0})
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DTranslate(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	x, y, err := canvasArgs2(context, arguments)
	if err != nil || !isFiniteFloat(x) || !isFiniteFloat(y) {
		return memory.UndefinedValue(), err
	}
	state.current.transform = state.current.transform.multiply(canvasMatrix{1, 0, 0, 1, x, y})
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DTransform(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	vals, ok, err := canvasArgs6(context, arguments)
	if err != nil || !ok {
		return memory.UndefinedValue(), err
	}
	state.current.transform = state.current.transform.multiply(vals)
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DSetTransform(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if len(arguments) <= 1 {
		if len(arguments) == 0 || arguments[0].Kind() == memory.ValueUndefined {
			state.current.transform = canvasIdentityMatrix
			return memory.UndefinedValue(), nil
		}
		if arguments[0].IsRef() {
			obj := arguments[0].Ref()
			m := canvasIdentityMatrix
			keys := [6]string{"a", "b", "c", "d", "e", "f"}
			valid := true
			for i, key := range keys {
				v, found, propErr := ownValue(context, obj, key)
				if propErr != nil {
					return memory.Value{}, propErr
				}
				if !found {
					valid = false
					break
				}
				n, err := canvasNumber(context, v)
				if err != nil {
					return memory.Value{}, err
				}
				if !isFiniteFloat(n) {
					valid = false
					break
				}
				m[i] = n
			}
			if valid {
				state.current.transform = m
			}
		}
		return memory.UndefinedValue(), nil
	}
	vals, ok, err := canvasArgs6(context, arguments)
	if err != nil || !ok {
		return memory.UndefinedValue(), err
	}
	state.current.transform = vals
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DResetTransform(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	state.current.transform = canvasIdentityMatrix
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DBeginPath(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	state.subpaths = nil
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DClosePath(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	state.closePath()
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DMoveTo(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	x, y, err := canvasArgs2(context, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	state.moveTo(x, y)
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DLineTo(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	x, y, err := canvasArgs2(context, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	state.lineTo(x, y)
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DRect(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	x, y, w, h, err := canvasArgs4(context, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	state.rect(x, y, w, h)
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DRoundRect(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	x, y, w, h, err := canvasArgs4(context, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	if !isFiniteFloat(x) || !isFiniteFloat(y) || !isFiniteFloat(w) || !isFiniteFloat(h) {
		return memory.UndefinedValue(), nil
	}
	r := 0.0
	if arg4 := argument(arguments, 4); arg4.Kind() == memory.ValueNumber {
		r = math.Max(0, arg4.Number())
	} else if arg4.IsRef() {
		// Accept array-like [r] -> take first element.
		if elem, found, elemErr := ownValue(context, arg4.Ref(), "0"); elemErr == nil && found && elem.Kind() == memory.ValueNumber {
			r = math.Max(0, elem.Number())
		}
	}
	if r == 0 || !isFiniteFloat(r) {
		state.rect(x, y, w, h)
		return memory.UndefinedValue(), nil
	}
	if w < 0 {
		x += w
		w = -w
	}
	if h < 0 {
		y += h
		h = -h
	}
	rx := math.Min(r, w/2)
	ry := math.Min(r, h/2)
	r = math.Min(rx, ry)
	state.moveTo(x+r, y)
	state.lineTo(x+w-r, y)
	appendEllipticArc(state, x+w-r, y+r, r, r, 0, -math.Pi/2, 0, false)
	state.lineTo(x+w, y+h-r)
	appendEllipticArc(state, x+w-r, y+h-r, r, r, 0, 0, math.Pi/2, false)
	state.lineTo(x+r, y+h)
	appendEllipticArc(state, x+r, y+h-r, r, r, 0, math.Pi/2, math.Pi, false)
	state.lineTo(x, y+r)
	appendEllipticArc(state, x+r, y+r, r, r, 0, math.Pi, 3*math.Pi/2, false)
	state.closePath()
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DArc(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	x, err := canvasNumber(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	y, err := canvasNumber(context, argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	r, err := canvasNumber(context, argument(arguments, 2))
	if err != nil {
		return memory.Value{}, err
	}
	startAngle, err := canvasNumber(context, argument(arguments, 3))
	if err != nil {
		return memory.Value{}, err
	}
	endAngle, err := canvasNumber(context, argument(arguments, 4))
	if err != nil {
		return memory.Value{}, err
	}
	if !isFiniteFloat(x) || !isFiniteFloat(y) || !isFiniteFloat(r) || !isFiniteFloat(startAngle) || !isFiniteFloat(endAngle) {
		return memory.UndefinedValue(), nil
	}
	if r < 0 {
		return memory.Value{}, realm.throwDOMException(context, dom.NewException(dom.IndexSizeError, nil, "arc radius must not be negative"))
	}
	ccw := canvasBool(context, argument(arguments, 5))
	appendEllipticArc(state, x, y, r, r, 0, startAngle, endAngle, ccw)
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DEllipse(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	x, err := canvasNumber(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	y, err := canvasNumber(context, argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	rx, err := canvasNumber(context, argument(arguments, 2))
	if err != nil {
		return memory.Value{}, err
	}
	ry, err := canvasNumber(context, argument(arguments, 3))
	if err != nil {
		return memory.Value{}, err
	}
	rot, err := canvasNumber(context, argument(arguments, 4))
	if err != nil {
		return memory.Value{}, err
	}
	startAngle, err := canvasNumber(context, argument(arguments, 5))
	if err != nil {
		return memory.Value{}, err
	}
	endAngle, err := canvasNumber(context, argument(arguments, 6))
	if err != nil {
		return memory.Value{}, err
	}
	if !isFiniteFloat(x) || !isFiniteFloat(y) || !isFiniteFloat(rx) || !isFiniteFloat(ry) ||
		!isFiniteFloat(rot) || !isFiniteFloat(startAngle) || !isFiniteFloat(endAngle) {
		return memory.UndefinedValue(), nil
	}
	if rx < 0 || ry < 0 {
		return memory.Value{}, realm.throwDOMException(context, dom.NewException(dom.IndexSizeError, nil, "ellipse radii must not be negative"))
	}
	ccw := canvasBool(context, argument(arguments, 7))
	appendEllipticArc(state, x, y, rx, ry, rot, startAngle, endAngle, ccw)
	return memory.UndefinedValue(), nil
}

func appendEllipticArc(state *canvas2DState, cx, cy, rx, ry, rotation, startAngle, endAngle float64, ccw bool) {
	delta := endAngle - startAngle
	if !ccw && delta >= 2*math.Pi {
		delta = 2 * math.Pi
	} else if ccw && delta <= -2*math.Pi {
		delta = -2 * math.Pi
	} else {
		if !ccw {
			for delta < 0 {
				delta += 2 * math.Pi
			}
			for delta > 2*math.Pi {
				delta -= 2 * math.Pi
			}
		} else {
			for delta > 0 {
				delta -= 2 * math.Pi
			}
			for delta < -2*math.Pi {
				delta += 2 * math.Pi
			}
		}
	}
	maxR := math.Max(rx, ry) * state.current.transform.effectiveScale()
	steps := int(math.Ceil(math.Abs(delta) * math.Max(maxR, 1) / 2.0))
	if steps < 12 {
		steps = 12
	}
	if steps > 512 {
		steps = 512
	}
	sinRot, cosRot := math.Sincos(rotation)
	for i := 0; i <= steps; i++ {
		t := startAngle + delta*(float64(i)/float64(steps))
		ex := rx * math.Cos(t)
		ey := ry * math.Sin(t)
		lx := cx + ex*cosRot - ey*sinRot
		ly := cy + ex*sinRot + ey*cosRot
		state.lineTo(lx, ly)
	}
}

func (realm *Realm) canvas2DArcTo(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	x1, y1, err := canvasArgs2(context, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	x2, err := canvasNumber(context, argument(arguments, 2))
	if err != nil {
		return memory.Value{}, err
	}
	y2, err := canvasNumber(context, argument(arguments, 3))
	if err != nil {
		return memory.Value{}, err
	}
	radius, err := canvasNumber(context, argument(arguments, 4))
	if err != nil {
		return memory.Value{}, err
	}
	if !isFiniteFloat(x1) || !isFiniteFloat(y1) || !isFiniteFloat(x2) || !isFiniteFloat(y2) || !isFiniteFloat(radius) {
		return memory.UndefinedValue(), nil
	}
	if radius < 0 {
		return memory.Value{}, realm.throwDOMException(context, dom.NewException(dom.IndexSizeError, nil, "arcTo radius must not be negative"))
	}
	if len(state.subpaths) == 0 || len(state.subpaths[len(state.subpaths)-1].points) == 0 {
		state.moveTo(x1, y1)
		return memory.UndefinedValue(), nil
	}
	p1 := state.current.transform.transformPoint(x1, y1)
	p2 := state.current.transform.transformPoint(x2, y2)
	sub := &state.subpaths[len(state.subpaths)-1]
	p0 := sub.points[len(sub.points)-1]
	if radius == 0 || (p0.x == p1.x && p0.y == p1.y) || (p1.x == p2.x && p1.y == p2.y) {
		sub.points = append(sub.points, p1)
		return memory.UndefinedValue(), nil
	}
	v1x, v1y := p0.x-p1.x, p0.y-p1.y
	v2x, v2y := p2.x-p1.x, p2.y-p1.y
	len1 := math.Hypot(v1x, v1y)
	len2 := math.Hypot(v2x, v2y)
	v1x /= len1
	v1y /= len1
	v2x /= len2
	v2y /= len2
	cross := v1x*v2y - v1y*v2x
	if math.Abs(cross) < 1e-7 {
		sub.points = append(sub.points, p1)
		return memory.UndefinedValue(), nil
	}
	dot := math.Max(-1, math.Min(1, v1x*v2x+v1y*v2y))
	angle := math.Acos(dot)
	rScaled := radius * state.current.transform.effectiveScale()
	tangentDist := rScaled / math.Tan(angle/2)
	t1 := canvasPoint{x: p1.x + v1x*tangentDist, y: p1.y + v1y*tangentDist}
	t2 := canvasPoint{x: p1.x + v2x*tangentDist, y: p1.y + v2y*tangentDist}
	bisx, bisy := v1x+v2x, v1y+v2y
	bisLen := math.Hypot(bisx, bisy)
	centerDist := rScaled / math.Sin(angle/2)
	cx := p1.x + (bisx/bisLen)*centerDist
	cy := p1.y + (bisy/bisLen)*centerDist
	a1 := math.Atan2(t1.y-cy, t1.x-cx)
	a2 := math.Atan2(t2.y-cy, t2.x-cx)
	ccw := cross > 0
	delta := a2 - a1
	if ccw {
		for delta > 0 {
			delta -= 2 * math.Pi
		}
	} else {
		for delta < 0 {
			delta += 2 * math.Pi
		}
	}
	steps := int(math.Ceil(math.Abs(delta) * math.Max(rScaled, 1) / 2.0))
	if steps < 8 {
		steps = 8
	}
	if steps > 128 {
		steps = 128
	}
	for i := 0; i <= steps; i++ {
		a := a1 + delta*(float64(i)/float64(steps))
		sub.points = append(sub.points, canvasPoint{
			x: cx + rScaled*math.Cos(a),
			y: cy + rScaled*math.Sin(a),
		})
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DQuadraticCurveTo(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	cpx, cpy, x, y, err := canvasArgs4(context, arguments)
	if err != nil || !isFiniteFloat(cpx) || !isFiniteFloat(cpy) || !isFiniteFloat(x) || !isFiniteFloat(y) {
		return memory.UndefinedValue(), err
	}
	sub := state.ensureSubpath(cpx, cpy)
	p0 := sub.points[len(sub.points)-1]
	p1 := state.current.transform.transformPoint(cpx, cpy)
	p2 := state.current.transform.transformPoint(x, y)
	flattenQuadBezier(&sub.points, p0, p1, p2, 0)
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DBezierCurveTo(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	cp1x, cp1y, cp2x, cp2y, x, y, err := canvasArgs6Raw(context, arguments)
	if err != nil || !isFiniteFloat(cp1x) || !isFiniteFloat(cp1y) || !isFiniteFloat(cp2x) || !isFiniteFloat(cp2y) || !isFiniteFloat(x) || !isFiniteFloat(y) {
		return memory.UndefinedValue(), err
	}
	sub := state.ensureSubpath(cp1x, cp1y)
	p0 := sub.points[len(sub.points)-1]
	p1 := state.current.transform.transformPoint(cp1x, cp1y)
	p2 := state.current.transform.transformPoint(cp2x, cp2y)
	p3 := state.current.transform.transformPoint(x, y)
	flattenCubicBezier(&sub.points, p0, p1, p2, p3, 0)
	return memory.UndefinedValue(), nil
}

func flattenQuadBezier(dst *[]canvasPoint, p0, p1, p2 canvasPoint, depth int) {
	if depth >= 10 || pointLineDist(p1, p0, p2) <= 0.25 {
		*dst = append(*dst, p2)
		return
	}
	p01 := midPoint(p0, p1)
	p12 := midPoint(p1, p2)
	p012 := midPoint(p01, p12)
	flattenQuadBezier(dst, p0, p01, p012, depth+1)
	flattenQuadBezier(dst, p012, p12, p2, depth+1)
}

func flattenCubicBezier(dst *[]canvasPoint, p0, p1, p2, p3 canvasPoint, depth int) {
	if depth >= 10 || (pointLineDist(p1, p0, p3) <= 0.25 && pointLineDist(p2, p0, p3) <= 0.25) {
		*dst = append(*dst, p3)
		return
	}
	p01 := midPoint(p0, p1)
	p12 := midPoint(p1, p2)
	p23 := midPoint(p2, p3)
	p012 := midPoint(p01, p12)
	p123 := midPoint(p12, p23)
	p0123 := midPoint(p012, p123)
	flattenCubicBezier(dst, p0, p01, p012, p0123, depth+1)
	flattenCubicBezier(dst, p0123, p123, p23, p3, depth+1)
}

func midPoint(a, b canvasPoint) canvasPoint {
	return canvasPoint{x: 0.5 * (a.x + b.x), y: 0.5 * (a.y + b.y)}
}

func pointLineDist(p, a, b canvasPoint) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	length := math.Hypot(dx, dy)
	if length < 1e-6 {
		return math.Hypot(p.x-a.x, p.y-a.y)
	}
	return math.Abs(dx*(a.y-p.y)-(a.x-p.x)*dy) / length
}

func (realm *Realm) canvas2DClearRect(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, host, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	x, y, w, h, err := canvasArgs4(context, arguments)
	if err != nil || !isFiniteFloat(x) || !isFiniteFloat(y) || !isFiniteFloat(w) || !isFiniteFloat(h) || w == 0 || h == 0 {
		return memory.UndefinedValue(), err
	}
	poly := transformedRectPoly(state.current.transform, x, y, w, h)
	rev, err := host.MutateCanvas(state.handle, func(width, height int, bitmap *image.RGBA) bool {
		if bitmap == nil || width <= 0 || height <= 0 {
			return false
		}
		if minX, minY, maxX, maxY, axisAligned := axisAlignedIntRect(poly, width, height); axisAligned {
			for py := minY; py < maxY; py++ {
				start := py*bitmap.Stride + minX*4
				end := py*bitmap.Stride + maxX*4
				clear(bitmap.Pix[start:end])
			}
			return maxX > minX && maxY > minY
		}
		mask := rasterizeContoursMask(bitmap.Bounds(), []canvasSubpath{{points: poly, closed: true}}, false)
		return applyClearMask(bitmap, mask)
	})
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	state.revision = rev
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DFillRect(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, host, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	x, y, w, h, err := canvasArgs4(context, arguments)
	if err != nil || !isFiniteFloat(x) || !isFiniteFloat(y) || !isFiniteFloat(w) || !isFiniteFloat(h) || w == 0 || h == 0 {
		return memory.UndefinedValue(), err
	}
	poly := transformedRectPoly(state.current.transform, x, y, w, h)
	col := effectiveCanvasColor(state.current.fillColor, state.current.globalAlpha)
	op := state.current.globalCompositeOperation
	rev, err := host.MutateCanvas(state.handle, func(width, height int, bitmap *image.RGBA) bool {
		if bitmap == nil || width <= 0 || height <= 0 {
			return false
		}
		mask := rasterizeContoursMask(bitmap.Bounds(), []canvasSubpath{{points: poly, closed: true}}, false)
		return compositeAlphaMask(bitmap, mask, col, op)
	})
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	state.revision = rev
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DStrokeRect(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, host, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	x, y, w, h, err := canvasArgs4(context, arguments)
	if err != nil || !isFiniteFloat(x) || !isFiniteFloat(y) || !isFiniteFloat(w) || !isFiniteFloat(h) {
		return memory.UndefinedValue(), err
	}
	if w == 0 && h == 0 {
		return memory.UndefinedValue(), nil
	}
	poly := transformedRectPoly(state.current.transform, x, y, w, h)
	contours := expandStrokeContours([]canvasSubpath{{points: poly, closed: true}}, state.current)
	col := effectiveCanvasColor(state.current.strokeColor, state.current.globalAlpha)
	op := state.current.globalCompositeOperation
	rev, err := host.MutateCanvas(state.handle, func(width, height int, bitmap *image.RGBA) bool {
		if bitmap == nil || width <= 0 || height <= 0 || len(contours) == 0 {
			return false
		}
		mask := rasterizeContoursMask(bitmap.Bounds(), contours, false)
		return compositeAlphaMask(bitmap, mask, col, op)
	})
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	state.revision = rev
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DFill(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, host, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	evenOdd := false
	if arg0 := argument(arguments, 0); isStringValue(context, arg0) {
		rule, strErr := valueString(context, arg0)
		if strErr != nil {
			return memory.Value{}, strErr
		}
		evenOdd = rule == "evenodd"
	}
	subpaths := make([]canvasSubpath, 0, len(state.subpaths))
	for _, sub := range state.subpaths {
		if len(sub.points) >= 3 {
			subpaths = append(subpaths, canvasSubpath{
				points: append([]canvasPoint(nil), sub.points...),
				closed: true,
			})
		}
	}
	if len(subpaths) == 0 {
		return memory.UndefinedValue(), nil
	}
	col := effectiveCanvasColor(state.current.fillColor, state.current.globalAlpha)
	op := state.current.globalCompositeOperation
	rev, err := host.MutateCanvas(state.handle, func(width, height int, bitmap *image.RGBA) bool {
		if bitmap == nil || width <= 0 || height <= 0 {
			return false
		}
		mask := rasterizeContoursMask(bitmap.Bounds(), subpaths, evenOdd)
		return compositeAlphaMask(bitmap, mask, col, op)
	})
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	state.revision = rev
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DStroke(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, host, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	contours := expandStrokeContours(state.subpaths, state.current)
	if len(contours) == 0 {
		return memory.UndefinedValue(), nil
	}
	col := effectiveCanvasColor(state.current.strokeColor, state.current.globalAlpha)
	op := state.current.globalCompositeOperation
	rev, err := host.MutateCanvas(state.handle, func(width, height int, bitmap *image.RGBA) bool {
		if bitmap == nil || width <= 0 || height <= 0 {
			return false
		}
		mask := rasterizeContoursMask(bitmap.Bounds(), contours, false)
		return compositeAlphaMask(bitmap, mask, col, op)
	})
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	state.revision = rev
	return memory.UndefinedValue(), nil
}

func transformedRectPoly(m canvasMatrix, x, y, w, h float64) []canvasPoint {
	if w < 0 {
		x += w
		w = -w
	}
	if h < 0 {
		y += h
		h = -h
	}
	return []canvasPoint{
		m.transformPoint(x, y),
		m.transformPoint(x+w, y),
		m.transformPoint(x+w, y+h),
		m.transformPoint(x, y+h),
	}
}

func axisAlignedIntRect(poly []canvasPoint, maxW, maxH int) (int, int, int, int, bool) {
	if len(poly) != 4 {
		return 0, 0, 0, 0, false
	}
	if math.Abs(poly[0].y-poly[1].y) > 1e-5 || math.Abs(poly[1].x-poly[2].x) > 1e-5 ||
		math.Abs(poly[2].y-poly[3].y) > 1e-5 || math.Abs(poly[3].x-poly[0].x) > 1e-5 {
		return 0, 0, 0, 0, false
	}
	minX := int(math.Floor(math.Min(poly[0].x, poly[2].x) + 1e-5))
	maxX := int(math.Ceil(math.Max(poly[0].x, poly[2].x) - 1e-5))
	minY := int(math.Floor(math.Min(poly[0].y, poly[2].y) + 1e-5))
	maxY := int(math.Ceil(math.Max(poly[0].y, poly[2].y) - 1e-5))
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX > maxW {
		maxX = maxW
	}
	if maxY > maxH {
		maxY = maxH
	}
	return minX, minY, maxX, maxY, true
}

func effectiveCanvasColor(c color.NRGBA, globalAlpha float64) color.NRGBA {
	c.A = uint8(math.Round(float64(c.A) * math.Max(0, math.Min(1, globalAlpha))))
	return c
}

func ensureClockwiseContour(pts []canvasPoint) []canvasPoint {
	if len(pts) < 3 {
		return pts
	}
	area := 0.0
	for i := 0; i < len(pts); i++ {
		j := (i + 1) % len(pts)
		area += (pts[j].x - pts[i].x) * (pts[j].y + pts[i].y)
	}
	if area < 0 {
		reversed := make([]canvasPoint, len(pts))
		for i := range pts {
			reversed[i] = pts[len(pts)-1-i]
		}
		return reversed
	}
	return pts
}

func makeCircleContour(cx, cy, r float64) canvasSubpath {
	steps := int(math.Ceil(2 * math.Pi * math.Max(r, 1) / 2.0))
	if steps < 12 {
		steps = 12
	}
	if steps > 96 {
		steps = 96
	}
	pts := make([]canvasPoint, steps)
	for i := 0; i < steps; i++ {
		a := 2 * math.Pi * float64(i) / float64(steps)
		pts[i] = canvasPoint{x: cx + r*math.Cos(a), y: cy + r*math.Sin(a)}
	}
	return canvasSubpath{points: ensureClockwiseContour(pts), closed: true}
}

func makeSquareContour(cx, cy, half float64) canvasSubpath {
	pts := []canvasPoint{
		{cx - half, cy - half},
		{cx + half, cy - half},
		{cx + half, cy + half},
		{cx - half, cy + half},
	}
	return canvasSubpath{points: ensureClockwiseContour(pts), closed: true}
}

func expandStrokeContours(subpaths []canvasSubpath, state canvas2DDrawState) []canvasSubpath {
	hw := 0.5 * state.lineWidth * state.transform.effectiveScale()
	if hw <= 0 || !isFiniteFloat(hw) {
		return nil
	}
	var out []canvasSubpath
	for _, sub := range subpaths {
		if len(sub.points) == 0 {
			continue
		}
		// Deduplicate consecutive identical points while preserving whether any non-zero segment existed.
		pts := make([]canvasPoint, 0, len(sub.points))
		for _, p := range sub.points {
			if len(pts) == 0 || math.Hypot(p.x-pts[len(pts)-1].x, p.y-pts[len(pts)-1].y) > 1e-6 {
				pts = append(pts, p)
			}
		}
		if len(pts) == 1 {
			if sub.closed || len(sub.points) > 1 {
				switch state.lineCap {
				case "round":
					out = append(out, makeCircleContour(pts[0].x, pts[0].y, hw))
				case "square":
					out = append(out, makeSquareContour(pts[0].x, pts[0].y, hw))
				}
			}
			continue
		}
		segCount := len(pts) - 1
		if sub.closed && math.Hypot(pts[len(pts)-1].x-pts[0].x, pts[len(pts)-1].y-pts[0].y) > 1e-6 {
			segCount = len(pts)
		}
		for i := 0; i < segCount; i++ {
			a := pts[i]
			b := pts[(i+1)%len(pts)]
			dx, dy := b.x-a.x, b.y-a.y
			length := math.Hypot(dx, dy)
			if length < 1e-6 {
				continue
			}
			ux, uy := dx/length, dy/length
			nx, ny := -uy*hw, ux*hw
			sa, sb := a, b
			if !sub.closed && state.lineCap == "square" {
				if i == 0 {
					sa = canvasPoint{a.x - ux*hw, a.y - uy*hw}
				}
				if i == segCount-1 {
					sb = canvasPoint{b.x + ux*hw, b.y + uy*hw}
				}
			}
			quad := ensureClockwiseContour([]canvasPoint{
				{sa.x + nx, sa.y + ny},
				{sb.x + nx, sb.y + ny},
				{sb.x - nx, sb.y - ny},
				{sa.x - nx, sa.y - ny},
			})
			out = append(out, canvasSubpath{points: quad, closed: true})

			// Joins between consecutive segments.
			if i < segCount-1 || sub.closed {
				c := pts[(i+2)%len(pts)]
				nd2x, nd2y := c.x-b.x, c.y-b.y
				len2 := math.Hypot(nd2x, nd2y)
				if len2 >= 1e-6 {
					out = append(out, buildJoinContour(b, ux, uy, nd2x/len2, nd2y/len2, hw, state.lineJoin, state.miterLimit)...)
				}
			}
		}
		if !sub.closed && state.lineCap == "round" {
			out = append(out, makeCircleContour(pts[0].x, pts[0].y, hw))
			out = append(out, makeCircleContour(pts[len(pts)-1].x, pts[len(pts)-1].y, hw))
		}
	}
	return out
}

func buildJoinContour(vertex canvasPoint, u1x, u1y, u2x, u2y, hw float64, join string, miterLimit float64) []canvasSubpath {
	if join == "round" {
		return []canvasSubpath{makeCircleContour(vertex.x, vertex.y, hw)}
	}
	cross := u1x*u2y - u1y*u2x
	if math.Abs(cross) < 1e-5 {
		return nil
	}
	sign := 1.0
	if cross > 0 {
		sign = -1.0
	}
	n1x, n1y := -u1y*hw*sign, u1x*hw*sign
	n2x, n2y := -u2y*hw*sign, u2x*hw*sign
	p1 := canvasPoint{vertex.x + n1x, vertex.y + n1y}
	p2 := canvasPoint{vertex.x + n2x, vertex.y + n2y}

	if join == "miter" {
		// Intersect lines (p1 + t*u1) and (p2 - s*u2).
		denom := u1x*u2y - u1y*u2x
		if math.Abs(denom) > 1e-6 {
			dx, dy := p2.x-p1.x, p2.y-p1.y
			t := (dx*u2y - dy*u2x) / denom
			miterPt := canvasPoint{p1.x + t*u1x, p1.y + t*u1y}
			dist := math.Hypot(miterPt.x-vertex.x, miterPt.y-vertex.y)
			if hw > 0 && dist/hw <= miterLimit {
				pts := ensureClockwiseContour([]canvasPoint{vertex, p1, miterPt, p2})
				return []canvasSubpath{{points: pts, closed: true}}
			}
		}
	}
	// Bevel (or miter fallback).
	pts := ensureClockwiseContour([]canvasPoint{vertex, p1, p2})
	return []canvasSubpath{{points: pts, closed: true}}
}

func rasterizeContoursMask(bounds image.Rectangle, contours []canvasSubpath, evenOdd bool) *image.Alpha {
	w, h := bounds.Dx(), bounds.Dy()
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 || len(contours) == 0 {
		return mask
	}
	if evenOdd {
		rasterizeEvenOddMask(mask, contours)
		return mask
	}
	rast := vector.NewRasterizer(w, h)
	hasEdges := false
	for _, c := range contours {
		if len(c.points) < 3 {
			continue
		}
		hasEdges = true
		rast.MoveTo(float32(c.points[0].x), float32(c.points[0].y))
		for i := 1; i < len(c.points); i++ {
			rast.LineTo(float32(c.points[i].x), float32(c.points[i].y))
		}
		rast.ClosePath()
	}
	if !hasEdges {
		return mask
	}
	rast.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	return mask
}

func rasterizeEvenOddMask(mask *image.Alpha, contours []canvasSubpath) {
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	type edge struct{ x0, y0, x1, y1 float64 }
	var edges []edge
	for _, c := range contours {
		if len(c.points) < 3 {
			continue
		}
		for i := 0; i < len(c.points); i++ {
			a := c.points[i]
			b := c.points[(i+1)%len(c.points)]
			if a.y == b.y {
				continue
			}
			if a.y > b.y {
				a, b = b, a
			}
			edges = append(edges, edge{a.x, a.y, b.x, b.y})
		}
	}
	if len(edges) == 0 {
		return
	}
	xs := make([]float64, 0, 16)
	for py := 0; py < h; py++ {
		y := float64(py) + 0.5
		xs = xs[:0]
		for _, e := range edges {
			if y >= e.y0 && y < e.y1 {
				t := (y - e.y0) / (e.y1 - e.y0)
				xs = append(xs, e.x0+t*(e.x1-e.x0))
			}
		}
		if len(xs) < 2 {
			continue
		}
		// Insertion sort small intersection lists per scanline.
		for i := 1; i < len(xs); i++ {
			cur := xs[i]
			j := i - 1
			for j >= 0 && xs[j] > cur {
				xs[j+1] = xs[j]
				j--
			}
			xs[j+1] = cur
		}
		for i := 0; i+1 < len(xs); i += 2 {
			start := int(math.Ceil(xs[i] - 0.5))
			end := int(math.Floor(xs[i+1] + 0.5))
			if start < 0 {
				start = 0
			}
			if end > w {
				end = w
			}
			rowOffset := py * mask.Stride
			for px := start; px < end; px++ {
				mask.Pix[rowOffset+px] = 255
			}
		}
	}
}

func applyClearMask(bitmap *image.RGBA, mask *image.Alpha) bool {
	bounds := bitmap.Bounds().Intersect(mask.Bounds())
	changed := false
	for py := bounds.Min.Y; py < bounds.Max.Y; py++ {
		for px := bounds.Min.X; px < bounds.Max.X; px++ {
			cov := mask.AlphaAt(px, py).A
			if cov == 0 {
				continue
			}
			off := bitmap.PixOffset(px, py)
			if bitmap.Pix[off+3] == 0 {
				continue
			}
			if cov == 255 {
				bitmap.Pix[off] = 0
				bitmap.Pix[off+1] = 0
				bitmap.Pix[off+2] = 0
				bitmap.Pix[off+3] = 0
			} else {
				inv := uint32(255 - cov)
				bitmap.Pix[off] = uint8((uint32(bitmap.Pix[off])*inv + 127) / 255)
				bitmap.Pix[off+1] = uint8((uint32(bitmap.Pix[off+1])*inv + 127) / 255)
				bitmap.Pix[off+2] = uint8((uint32(bitmap.Pix[off+2])*inv + 127) / 255)
				bitmap.Pix[off+3] = uint8((uint32(bitmap.Pix[off+3])*inv + 127) / 255)
			}
			changed = true
		}
	}
	return changed
}

func compositeAlphaMask(bitmap *image.RGBA, mask *image.Alpha, col color.NRGBA, op string) bool {
	if op == "destination-out" {
		if col.A == 0 {
			return false
		}
		if col.A == 255 {
			return applyClearMask(bitmap, mask)
		}
	}
	bounds := bitmap.Bounds().Intersect(mask.Bounds())
	changed := false
	for py := bounds.Min.Y; py < bounds.Max.Y; py++ {
		for px := bounds.Min.X; px < bounds.Max.X; px++ {
			cov := uint32(mask.AlphaAt(px, py).A)
			if cov == 0 {
				continue
			}
			srcA := (uint32(col.A)*cov + 127) / 255
			if srcA == 0 && op != "copy" {
				continue
			}
			srcR := (uint32(col.R)*srcA + 127) / 255
			srcG := (uint32(col.G)*srcA + 127) / 255
			srcB := (uint32(col.B)*srcA + 127) / 255
			off := bitmap.PixOffset(px, py)
			dstR := uint32(bitmap.Pix[off])
			dstG := uint32(bitmap.Pix[off+1])
			dstB := uint32(bitmap.Pix[off+2])
			dstA := uint32(bitmap.Pix[off+3])

			var outR, outG, outB, outA uint32
			switch op {
			case "copy":
				// Inside shape coverage blends cleanly over transparent; unmasked pixels keep dst.
				inv := 255 - cov
				outR = srcR + (dstR*inv+127)/255
				outG = srcG + (dstG*inv+127)/255
				outB = srcB + (dstB*inv+127)/255
				outA = srcA + (dstA*inv+127)/255
			case "destination-out":
				inv := 255 - srcA
				outR = (dstR*inv + 127) / 255
				outG = (dstG*inv + 127) / 255
				outB = (dstB*inv + 127) / 255
				outA = (dstA*inv + 127) / 255
			case "source-in":
				outR = (srcR*dstA + 127) / 255
				outG = (srcG*dstA + 127) / 255
				outB = (srcB*dstA + 127) / 255
				outA = (srcA*dstA + 127) / 255
			case "source-atop":
				inv := 255 - srcA
				outR = (srcR*dstA+dstR*inv+127)/255
				outG = (srcG*dstA+dstG*inv+127)/255
				outB = (srcB*dstA+dstB*inv+127)/255
				outA = dstA
			case "lighter":
				outR = min(255, srcR+dstR)
				outG = min(255, srcG+dstG)
				outB = min(255, srcB+dstB)
				outA = min(255, srcA+dstA)
			default: // "source-over"
				inv := 255 - srcA
				outR = srcR + (dstR*inv+127)/255
				outG = srcG + (dstG*inv+127)/255
				outB = srcB + (dstB*inv+127)/255
				outA = srcA + (dstA*inv+127)/255
			}
			bitmap.Pix[off] = uint8(outR)
			bitmap.Pix[off+1] = uint8(outG)
			bitmap.Pix[off+2] = uint8(outB)
			bitmap.Pix[off+3] = uint8(outA)
			changed = true
		}
	}
	return changed
}

func (realm *Realm) newImageDataConstructor(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, error) {
	return realm.newTextCodecConstructor(context, "ImageData", nativeImageDataConstructor)
}

func (realm *Realm) populateImageData(context *browserruntime.TaskContext, obj memory.Ref, width, height int, bytes []byte) error {
	buffer, err := context.NewArrayBuffer(bytes)
	if err != nil {
		return err
	}
	view, err := context.NewTypedArray(buffer, memory.ElementUint8Clamped, 0, uint64(len(bytes)))
	if err != nil {
		return err
	}
	if err := defineData(context, obj, "width", memory.NumberValue(float64(width)), false, true, false); err != nil {
		return err
	}
	if err := defineData(context, obj, "height", memory.NumberValue(float64(height)), false, true, false); err != nil {
		return err
	}
	return defineData(context, obj, "data", memory.RefValue(view), false, true, false)
}

func (realm *Realm) newImageDataInstance(context *browserruntime.TaskContext, width, height int, bytes []byte) (memory.Value, error) {
	obj, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetPrototype(obj, memory.RefValue(realm.bindings.imageDataPrototype)); err != nil {
		return memory.Value{}, err
	}
	if err := realm.populateImageData(context, obj, width, height, bytes); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(obj), nil
}

func (realm *Realm) imageDataConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: ImageData constructor requires new", browserruntime.ErrOperandType)
	}
	if len(arguments) < 2 {
		return memory.Value{}, fmt.Errorf("%w: ImageData requires at least 2 arguments", browserruntime.ErrOperandType)
	}
	arg0 := arguments[0]
	if arg0.IsRef() {
		view, err := context.DerefTypedArray(arg0.Ref())
		if err == nil && view.Element == memory.ElementUint8Clamped {
			wNum, err := canvasNumber(context, arguments[1])
			if err != nil {
				return memory.Value{}, err
			}
			w := int(math.Trunc(wNum))
			if !isFiniteFloat(wNum) || w <= 0 || view.Length%uint64(4*w) != 0 {
				return memory.Value{}, realm.throwDOMException(context, dom.NewException(dom.IndexSizeError, nil, "invalid ImageData dimensions"))
			}
			h := int(view.Length / uint64(4*w))
			if len(arguments) >= 3 && arguments[2].Kind() != memory.ValueUndefined {
				hNum, err := canvasNumber(context, arguments[2])
				if err != nil {
					return memory.Value{}, err
				}
				if int(math.Trunc(hNum)) != h {
					return memory.Value{}, realm.throwDOMException(context, dom.NewException(dom.IndexSizeError, nil, "ImageData height does not match buffer length"))
				}
			}
			raw, err := context.ReadArrayBuffer(view.Buffer, view.ByteOffset, view.Length)
			if err != nil {
				return memory.Value{}, err
			}
			if err := realm.populateImageData(context, this.Ref(), w, h, raw); err != nil {
				return memory.Value{}, err
			}
			return memory.UndefinedValue(), nil
		}
	}
	wNum, err := canvasNumber(context, arguments[0])
	if err != nil {
		return memory.Value{}, err
	}
	hNum, err := canvasNumber(context, arguments[1])
	if err != nil {
		return memory.Value{}, err
	}
	w := int(math.Trunc(wNum))
	h := int(math.Trunc(hNum))
	if !isFiniteFloat(wNum) || !isFiniteFloat(hNum) || w <= 0 || h <= 0 || w > dom.MaxCanvasDimension || h > dom.MaxCanvasDimension {
		return memory.Value{}, realm.throwDOMException(context, dom.NewException(dom.IndexSizeError, nil, "ImageData dimensions must be positive"))
	}
	if err := realm.populateImageData(context, this.Ref(), w, h, make([]byte, w*h*4)); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DCreateImageData(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if _, _, err := realm.requireCanvas2D(context, this); err != nil {
		return memory.Value{}, err
	}
	var w, h int
	if len(arguments) == 1 && arguments[0].IsRef() {
		obj := arguments[0].Ref()
		wv, foundW, err := ownValue(context, obj, "width")
		if err != nil {
			return memory.Value{}, err
		}
		hv, foundH, err := ownValue(context, obj, "height")
		if err != nil {
			return memory.Value{}, err
		}
		if !foundW || !foundH {
			return memory.Value{}, fmt.Errorf("%w: createImageData object requires width and height", browserruntime.ErrOperandType)
		}
		wn, err := canvasNumber(context, wv)
		if err != nil {
			return memory.Value{}, err
		}
		hn, err := canvasNumber(context, hv)
		if err != nil {
			return memory.Value{}, err
		}
		w, h = int(math.Trunc(wn)), int(math.Trunc(hn))
	} else {
		wn, hn, err := canvasArgs2(context, arguments)
		if err != nil {
			return memory.Value{}, err
		}
		w, h = int(math.Trunc(math.Abs(wn))), int(math.Trunc(math.Abs(hn)))
	}
	if w <= 0 || h <= 0 || w > dom.MaxCanvasDimension || h > dom.MaxCanvasDimension {
		return memory.Value{}, realm.throwDOMException(context, dom.NewException(dom.IndexSizeError, nil, "createImageData dimensions must be non-zero"))
	}
	return realm.newImageDataInstance(context, w, h, make([]byte, w*h*4))
}

func (realm *Realm) canvas2DGetImageData(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, host, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	sxf, syf, swf, shf, err := canvasArgs4(context, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	sx, sy, sw, sh := int(math.Trunc(sxf)), int(math.Trunc(syf)), int(math.Trunc(swf)), int(math.Trunc(shf))
	if sw == 0 || sh == 0 {
		return memory.Value{}, realm.throwDOMException(context, dom.NewException(dom.IndexSizeError, nil, "getImageData width and height must be non-zero"))
	}
	if sw < 0 {
		sx += sw
		sw = -sw
	}
	if sh < 0 {
		sy += sh
		sh = -sh
	}
	if sw > dom.MaxCanvasDimension || sh > dom.MaxCanvasDimension {
		return memory.Value{}, realm.throwDOMException(context, dom.NewException(dom.IndexSizeError, nil, "getImageData dimensions exceed limit"))
	}
	data := make([]byte, sw*sh*4)
	err = host.ReadCanvas(state.handle, func(width, height int, _ uint64, bitmap *image.RGBA) {
		if bitmap == nil || width <= 0 || height <= 0 {
			return
		}
		for row := 0; row < sh; row++ {
			by := sy + row
			if by < 0 || by >= height {
				continue
			}
			for col := 0; col < sw; col++ {
				bx := sx + col
				if bx < 0 || bx >= width {
					continue
				}
				srcOff := bitmap.PixOffset(bx, by)
				dstOff := (row*sw + col) * 4
				a := bitmap.Pix[srcOff+3]
				if a == 0 {
					continue
				}
				if a == 255 {
					data[dstOff] = bitmap.Pix[srcOff]
					data[dstOff+1] = bitmap.Pix[srcOff+1]
					data[dstOff+2] = bitmap.Pix[srcOff+2]
					data[dstOff+3] = 255
					continue
				}
				// Un-premultiply RGBA for straight ImageData bytes.
				af := uint32(a)
				r := (uint32(bitmap.Pix[srcOff])*255 + af/2) / af
				g := (uint32(bitmap.Pix[srcOff+1])*255 + af/2) / af
				b := (uint32(bitmap.Pix[srcOff+2])*255 + af/2) / af
				data[dstOff] = uint8(min(255, r))
				data[dstOff+1] = uint8(min(255, g))
				data[dstOff+2] = uint8(min(255, b))
				data[dstOff+3] = a
			}
		}
	})
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	return realm.newImageDataInstance(context, sw, sh, data)
}

func (realm *Realm) canvas2DPutImageData(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, host, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	imgData := argument(arguments, 0)
	if !imgData.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: putImageData requires an ImageData argument", browserruntime.ErrOperandType)
	}
	w, h, pixels, err := realm.extractImageData(context, imgData.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	dxf, err := canvasNumber(context, argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	dyf, err := canvasNumber(context, argument(arguments, 2))
	if err != nil {
		return memory.Value{}, err
	}
	dx, dy := int(math.Trunc(dxf)), int(math.Trunc(dyf))
	dirtyX, dirtyY, dirtyW, dirtyH := 0, 0, w, h
	if len(arguments) >= 7 {
		fx, fy, fw, fh, argErr := canvasArgs4(context, arguments[3:])
		if argErr != nil {
			return memory.Value{}, argErr
		}
		dirtyX, dirtyY, dirtyW, dirtyH = int(math.Trunc(fx)), int(math.Trunc(fy)), int(math.Trunc(fw)), int(math.Trunc(fh))
		if dirtyW < 0 {
			dirtyX += dirtyW
			dirtyW = -dirtyW
		}
		if dirtyH < 0 {
			dirtyY += dirtyH
			dirtyH = -dirtyH
		}
	}
	rev, err := host.MutateCanvas(state.handle, func(cw, ch int, bitmap *image.RGBA) bool {
		if bitmap == nil || cw <= 0 || ch <= 0 || dirtyW <= 0 || dirtyH <= 0 {
			return false
		}
		changed := false
		for row := dirtyY; row < dirtyY+dirtyH; row++ {
			if row < 0 || row >= h {
				continue
			}
			by := dy + row
			if by < 0 || by >= ch {
				continue
			}
			for col := dirtyX; col < dirtyX+dirtyW; col++ {
				if col < 0 || col >= w {
					continue
				}
				bx := dx + col
				if bx < 0 || bx >= cw {
					continue
				}
				srcOff := (row*w + col) * 4
				dstOff := bitmap.PixOffset(bx, by)
				a := uint32(pixels[srcOff+3])
				bitmap.Pix[dstOff] = uint8((uint32(pixels[srcOff])*a + 127) / 255)
				bitmap.Pix[dstOff+1] = uint8((uint32(pixels[srcOff+1])*a + 127) / 255)
				bitmap.Pix[dstOff+2] = uint8((uint32(pixels[srcOff+2])*a + 127) / 255)
				bitmap.Pix[dstOff+3] = uint8(a)
				changed = true
			}
		}
		return changed
	})
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	state.revision = rev
	return memory.UndefinedValue(), nil
}

func (realm *Realm) extractImageData(context *browserruntime.TaskContext, obj memory.Ref) (int, int, []byte, error) {
	wv, foundW, err := ownValue(context, obj, "width")
	if err != nil {
		return 0, 0, nil, err
	}
	hv, foundH, err := ownValue(context, obj, "height")
	if err != nil {
		return 0, 0, nil, err
	}
	dv, foundD, err := ownValue(context, obj, "data")
	if err != nil {
		return 0, 0, nil, err
	}
	if !foundW || !foundH || !foundD || !dv.IsRef() {
		return 0, 0, nil, fmt.Errorf("%w: ImageData requires width, height, and data", browserruntime.ErrOperandType)
	}
	view, err := context.DerefTypedArray(dv.Ref())
	if err != nil {
		return 0, 0, nil, err
	}
	raw, err := context.ReadArrayBuffer(view.Buffer, view.ByteOffset, view.Length)
	if err != nil {
		return 0, 0, nil, err
	}
	wn, err := canvasNumber(context, wv)
	if err != nil {
		return 0, 0, nil, err
	}
	hn, err := canvasNumber(context, hv)
	if err != nil {
		return 0, 0, nil, err
	}
	w, h := int(math.Trunc(wn)), int(math.Trunc(hn))
	if w <= 0 || h <= 0 || len(raw) < w*h*4 {
		return 0, 0, nil, fmt.Errorf("%w: invalid ImageData dimensions", browserruntime.ErrOperandType)
	}
	return w, h, raw, nil
}

func (realm *Realm) canvas2DDrawImage(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, host, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	srcHandle, _, err := realm.unwrapCanvasHandle(context, argument(arguments, 0))
	if err != nil {
		// Ignore unsupported image sources cleanly for now instead of throwing.
		return memory.UndefinedValue(), nil
	}
	var srcCopy *image.RGBA
	var srcW, srcH int
	if err := host.ReadCanvas(srcHandle, func(w, h int, _ uint64, bitmap *image.RGBA) {
		srcW, srcH = w, h
		if bitmap != nil && w > 0 && h > 0 {
			srcCopy = image.NewRGBA(bitmap.Bounds())
			copy(srcCopy.Pix, bitmap.Pix)
		}
	}); err != nil || srcW <= 0 || srcH <= 0 || srcCopy == nil {
		return memory.UndefinedValue(), nil
	}
	var sx, sy, sw, sh, dx, dy, dw, dh float64
	switch {
	case len(arguments) >= 9:
		sx, sy, sw, sh, err = canvasArgs4(context, arguments[1:5])
		if err != nil {
			return memory.Value{}, err
		}
		dx, dy, dw, dh, err = canvasArgs4(context, arguments[5:9])
		if err != nil {
			return memory.Value{}, err
		}
	case len(arguments) >= 5:
		sx, sy, sw, sh = 0, 0, float64(srcW), float64(srcH)
		dx, dy, dw, dh, err = canvasArgs4(context, arguments[1:5])
		if err != nil {
			return memory.Value{}, err
		}
	default:
		sx, sy, sw, sh = 0, 0, float64(srcW), float64(srcH)
		dx, dy, err = canvasArgs2(context, arguments[1:])
		if err != nil {
			return memory.Value{}, err
		}
		dw, dh = float64(srcW), float64(srcH)
	}
	if sw == 0 || sh == 0 || dw == 0 || dh == 0 {
		return memory.UndefinedValue(), nil
	}
	m := state.current.transform
	// Support scaled/translated axis-aligned blit.
	p0 := m.transformPoint(dx, dy)
	p1 := m.transformPoint(dx+dw, dy+dh)
	dstX0 := int(math.Round(math.Min(p0.x, p1.x)))
	dstY0 := int(math.Round(math.Min(p0.y, p1.y)))
	dstX1 := int(math.Round(math.Max(p0.x, p1.x)))
	dstY1 := int(math.Round(math.Max(p0.y, p1.y)))
	alphaScale := uint32(math.Round(state.current.globalAlpha * 255))
	rev, err := host.MutateCanvas(state.handle, func(cw, ch int, bitmap *image.RGBA) bool {
		if bitmap == nil || cw <= 0 || ch <= 0 || alphaScale == 0 {
			return false
		}
		changed := false
		dstW := dstX1 - dstX0
		dstH := dstY1 - dstY0
		if dstW <= 0 || dstH <= 0 {
			return false
		}
		for py := max(0, dstY0); py < min(ch, dstY1); py++ {
			v := (float64(py-dstY0) + 0.5) / float64(dstH)
			srcY := int(math.Floor(sy + v*sh))
			if srcY < 0 || srcY >= srcH {
				continue
			}
			for px := max(0, dstX0); px < min(cw, dstX1); px++ {
				u := (float64(px-dstX0) + 0.5) / float64(dstW)
				srcX := int(math.Floor(sx + u*sw))
				if srcX < 0 || srcX >= srcW {
					continue
				}
				sOff := srcCopy.PixOffset(srcX, srcY)
				sa := (uint32(srcCopy.Pix[sOff+3])*alphaScale + 127) / 255
				if sa == 0 {
					continue
				}
				sr := (uint32(srcCopy.Pix[sOff])*alphaScale + 127) / 255
				sg := (uint32(srcCopy.Pix[sOff+1])*alphaScale + 127) / 255
				sb := (uint32(srcCopy.Pix[sOff+2])*alphaScale + 127) / 255
				dOff := bitmap.PixOffset(px, py)
				inv := 255 - sa
				bitmap.Pix[dOff] = uint8(min(255, sr+(uint32(bitmap.Pix[dOff])*inv+127)/255))
				bitmap.Pix[dOff+1] = uint8(min(255, sg+(uint32(bitmap.Pix[dOff+1])*inv+127)/255))
				bitmap.Pix[dOff+2] = uint8(min(255, sb+(uint32(bitmap.Pix[dOff+2])*inv+127)/255))
				bitmap.Pix[dOff+3] = uint8(min(255, sa+(uint32(bitmap.Pix[dOff+3])*inv+127)/255))
				changed = true
			}
		}
		return changed
	})
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	state.revision = rev
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DFillText(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, _, err := realm.requireCanvas2D(context, this); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DStrokeText(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, _, err := realm.requireCanvas2D(context, this); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) canvas2DMeasureText(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, _, err := realm.requireCanvas2D(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	text, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	fontSize := parseCanvasFontSize(state.current.font)
	width := float64(len([]rune(text))) * fontSize * 0.6
	obj, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, obj, "width", memory.NumberValue(width), true, true, true); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(obj), nil
}

func parseCanvasFontSize(font string) float64 {
	for _, field := range strings.Fields(font) {
		if strings.HasSuffix(field, "px") {
			if n, err := strconv.ParseFloat(strings.TrimSuffix(field, "px"), 64); err == nil && n > 0 {
				return n
			}
		}
	}
	return 10
}

func isStringValue(context *browserruntime.TaskContext, val memory.Value) bool {
	if !val.IsRef() {
		return false
	}
	kind, err := context.HeapKind(val.Ref())
	return err == nil && kind == memory.HeapString
}

func canvasNumber(context *browserruntime.TaskContext, val memory.Value) (float64, error) {
	if isStringValue(context, val) {
		text, err := valueString(context, val)
		if err != nil {
			return 0, err
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return 0, nil
		}
		n, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return math.NaN(), nil
		}
		return n, nil
	}
	return typedArrayNumber(context, val)
}

func canvasBool(context *browserruntime.TaskContext, val memory.Value) bool {
	switch val.Kind() {
	case memory.ValueUndefined, memory.ValueNull:
		return false
	case memory.ValueBool:
		return val.Bool()
	case memory.ValueNumber:
		n := val.Number()
		return n != 0 && !math.IsNaN(n)
	default:
		if val.IsRef() && isStringValue(context, val) {
			s, err := valueString(context, val)
			return err == nil && s != ""
		}
		return true
	}
}

func canvasArgs2(context *browserruntime.TaskContext, arguments []memory.Value) (float64, float64, error) {
	a, err := canvasNumber(context, argument(arguments, 0))
	if err != nil {
		return 0, 0, err
	}
	b, err := canvasNumber(context, argument(arguments, 1))
	return a, b, err
}

func canvasArgs4(context *browserruntime.TaskContext, arguments []memory.Value) (float64, float64, float64, float64, error) {
	a, b, err := canvasArgs2(context, arguments)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	c, err := canvasNumber(context, argument(arguments, 2))
	if err != nil {
		return 0, 0, 0, 0, err
	}
	d, err := canvasNumber(context, argument(arguments, 3))
	return a, b, c, d, err
}

func canvasArgs6(context *browserruntime.TaskContext, arguments []memory.Value) (canvasMatrix, bool, error) {
	a, b, c, d, e, f, err := canvasArgs6Raw(context, arguments)
	if err != nil {
		return canvasMatrix{}, false, err
	}
	if !isFiniteFloat(a) || !isFiniteFloat(b) || !isFiniteFloat(c) || !isFiniteFloat(d) || !isFiniteFloat(e) || !isFiniteFloat(f) {
		return canvasMatrix{}, false, nil
	}
	return canvasMatrix{a, b, c, d, e, f}, true, nil
}

func canvasArgs6Raw(context *browserruntime.TaskContext, arguments []memory.Value) (float64, float64, float64, float64, float64, float64, error) {
	a, b, c, d, err := canvasArgs4(context, arguments)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}
	e, err := canvasNumber(context, argument(arguments, 4))
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}
	f, err := canvasNumber(context, argument(arguments, 5))
	return a, b, c, d, e, f, err
}
