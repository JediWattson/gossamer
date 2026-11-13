package render

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
	"golang.org/x/image/vector"
)

const (
	svgMaxRasterDim   = 1024
	svgRasterScale    = 2.0
	svgFlattenTol     = 0.25
	svgMaxSubdivDepth = 10
)

func isSVGElement(node *dom.Node) bool {
	return node != nil && node.Type == dom.ElementNode && node.NamespaceURI == dom.SVGNamespace && node.Data == "svg"
}

func svgAttr(node *dom.Node, name string) (string, bool) {
	if node == nil {
		return "", false
	}
	for _, attribute := range node.Attributes {
		if strings.EqualFold(attribute.Name, name) {
			return attribute.Value, true
		}
	}
	return "", false
}

func parseSVGViewBox(node *dom.Node) (minX, minY, width, height float64, ok bool) {
	raw, found := svgAttr(node, "viewBox")
	if !found {
		return 0, 0, 0, 0, false
	}
	nums := parseSVGNumberList(raw)
	if len(nums) != 4 || nums[2] <= 0 || nums[3] <= 0 {
		return 0, 0, 0, 0, false
	}
	return nums[0], nums[1], nums[2], nums[3], true
}

func parseSVGLengthAttr(node *dom.Node, name string) (float64, bool) {
	raw, found := svgAttr(node, name)
	if !found {
		return 0, false
	}
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, "px")
	trimmed = strings.TrimSpace(trimmed)
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || !isFinite(value) || value < 0 {
		return 0, false
	}
	return value, true
}

func svgIntrinsicDimensions(node *dom.Node) (float64, float64, bool) {
	if !isSVGElement(node) {
		return 0, 0, false
	}
	_, _, vbW, vbH, hasViewBox := parseSVGViewBox(node)
	attrW, hasAttrW := parseSVGLengthAttr(node, "width")
	attrH, hasAttrH := parseSVGLengthAttr(node, "height")
	switch {
	case hasAttrW && hasAttrH && attrW > 0 && attrH > 0:
		return attrW, attrH, true
	case hasViewBox:
		return vbW, vbH, true
	case hasAttrW && attrW > 0:
		return attrW, attrW / 2, true
	case hasAttrH && attrH > 0:
		return attrH * 2, attrH, true
	default:
		return 300, 150, true
	}
}

type svgMatrix struct {
	a, b, c, d, e, f float64
}

func svgIdentityMatrix() svgMatrix {
	return svgMatrix{a: 1, d: 1}
}

func (m svgMatrix) mul(o svgMatrix) svgMatrix {
	return svgMatrix{
		a: m.a*o.a + m.c*o.b,
		b: m.b*o.a + m.d*o.b,
		c: m.a*o.c + m.c*o.d,
		d: m.b*o.c + m.d*o.d,
		e: m.a*o.e + m.c*o.f + m.e,
		f: m.b*o.e + m.d*o.f + m.f,
	}
}

func (m svgMatrix) apply(p svgPoint) svgPoint {
	return svgPoint{
		x: m.a*p.x + m.c*p.y + m.e,
		y: m.b*p.x + m.d*p.y + m.f,
	}
}

func (m svgMatrix) avgScale() float64 {
	det := math.Abs(m.a*m.d - m.b*m.c)
	if det <= 0 || !isFinite(det) {
		return 1
	}
	return math.Sqrt(det)
}

type svgPoint struct {
	x, y float64
}

type svgSubpath struct {
	points []svgPoint
	closed bool
}

type svgPaintState struct {
	currentColor   color.NRGBA
	fill           color.NRGBA
	hasFill        bool
	fillOpacity    float64
	stroke         color.NRGBA
	hasStroke      bool
	strokeOpacity  float64
	strokeWidth    float64
	strokeLinecap  string
	strokeLinejoin string
	opacity        float64
	transform      svgMatrix
}

func renderSVGNode(
	node *dom.Node,
	svgStyle computedStyle,
	styles map[*dom.Node]computedStyle,
	cssWidth float64,
	cssHeight float64,
) image.Image {
	return renderSVGNodeAtScale(node, svgStyle, styles, cssWidth, cssHeight, svgRasterScale)
}

func renderSVGNodeAtScale(
	node *dom.Node,
	svgStyle computedStyle,
	styles map[*dom.Node]computedStyle,
	cssWidth float64,
	cssHeight float64,
	rasterScale float64,
) image.Image {
	if !isSVGElement(node) || cssWidth <= 0 || cssHeight <= 0 || !isFinite(cssWidth) || !isFinite(cssHeight) || rasterScale <= 0 {
		return nil
	}
	pw := int(math.Ceil(cssWidth * rasterScale))
	ph := int(math.Ceil(cssHeight * rasterScale))
	if pw <= 0 || ph <= 0 {
		return nil
	}
	if pw > svgMaxRasterDim {
		factor := float64(svgMaxRasterDim) / float64(pw)
		pw = svgMaxRasterDim
		ph = max(1, int(math.Round(float64(ph)*factor)))
	}
	if ph > svgMaxRasterDim {
		factor := float64(svgMaxRasterDim) / float64(ph)
		ph = svgMaxRasterDim
		pw = max(1, int(math.Round(float64(pw)*factor)))
	}

	vbX, vbY, vbW, vbH, hasViewBox := parseSVGViewBox(node)
	if !hasViewBox {
		vbX, vbY = 0, 0
		vbW, vbH = cssWidth, cssHeight
		if attrW, ok := parseSVGLengthAttr(node, "width"); ok && attrW > 0 {
			vbW = attrW
		}
		if attrH, ok := parseSVGLengthAttr(node, "height"); ok && attrH > 0 {
			vbH = attrH
		}
	}
	if vbW <= 0 || vbH <= 0 {
		return nil
	}

	sx := float64(pw) / vbW
	sy := float64(ph) / vbH
	uniformScale := math.Min(sx, sy)
	if par, ok := svgAttr(node, "preserveAspectRatio"); ok && strings.EqualFold(strings.TrimSpace(par), "none") {
		viewTransform := svgMatrix{
			a: sx, d: sy,
			e: -vbX * sx,
			f: -vbY * sy,
		}
		dst := image.NewRGBA(image.Rect(0, 0, pw, ph))
		rootState := initialSVGState(node, svgStyle.Color(), viewTransform)
		renderSVGChildren(dst, node, rootState, styles)
		return dst
	}

	tx := (float64(pw)-vbW*uniformScale)/2 - vbX*uniformScale
	ty := (float64(ph)-vbH*uniformScale)/2 - vbY*uniformScale
	viewTransform := svgMatrix{
		a: uniformScale, d: uniformScale,
		e: tx, f: ty,
	}

	dst := image.NewRGBA(image.Rect(0, 0, pw, ph))
	rootState := initialSVGState(node, svgStyle.Color(), viewTransform)
	renderSVGChildren(dst, node, rootState, styles)
	return dst
}

func initialSVGState(node *dom.Node, currentColor color.NRGBA, transform svgMatrix) svgPaintState {
	state := svgPaintState{
		currentColor:   currentColor,
		fill:           color.NRGBA{A: 255},
		hasFill:        true,
		fillOpacity:    1,
		strokeOpacity:  1,
		strokeWidth:    1,
		strokeLinecap:  "butt",
		strokeLinejoin: "miter",
		opacity:        1,
		transform:      transform,
	}
	return applySVGElementState(node, state, false)
}

func applySVGElementState(node *dom.Node, inherited svgPaintState, includeTransform bool) svgPaintState {
	state := inherited
	properties := collectSVGProperties(node)

	if rawColor, ok := properties["color"]; ok {
		if parsed, valid := parseSVGPaintColor(rawColor, state.currentColor); valid {
			state.currentColor = parsed
		}
	}
	if rawOpacity, ok := properties["opacity"]; ok {
		if parsed, valid := parseSVGFloat(rawOpacity); valid {
			state.opacity *= clamp(parsed, 0, 1)
		}
	}
	if rawFill, ok := properties["fill"]; ok {
		trimmed := strings.TrimSpace(rawFill)
		if strings.EqualFold(trimmed, "none") {
			state.hasFill = false
		} else if parsed, valid := parseSVGPaintColor(trimmed, state.currentColor); valid {
			state.fill = parsed
			state.hasFill = true
		}
	}
	if rawFillOpacity, ok := properties["fill-opacity"]; ok {
		if parsed, valid := parseSVGFloat(rawFillOpacity); valid {
			state.fillOpacity = clamp(parsed, 0, 1)
		}
	}
	if rawStroke, ok := properties["stroke"]; ok {
		trimmed := strings.TrimSpace(rawStroke)
		if strings.EqualFold(trimmed, "none") {
			state.hasStroke = false
		} else if parsed, valid := parseSVGPaintColor(trimmed, state.currentColor); valid {
			state.stroke = parsed
			state.hasStroke = true
		}
	}
	if rawStrokeOpacity, ok := properties["stroke-opacity"]; ok {
		if parsed, valid := parseSVGFloat(rawStrokeOpacity); valid {
			state.strokeOpacity = clamp(parsed, 0, 1)
		}
	}
	if rawStrokeWidth, ok := properties["stroke-width"]; ok {
		trimmed := strings.TrimSuffix(strings.TrimSpace(rawStrokeWidth), "px")
		if parsed, valid := parseSVGFloat(trimmed); valid && parsed >= 0 {
			state.strokeWidth = parsed
		}
	}
	if rawCap, ok := properties["stroke-linecap"]; ok {
		capValue := strings.ToLower(strings.TrimSpace(rawCap))
		if capValue == "butt" || capValue == "round" || capValue == "square" {
			state.strokeLinecap = capValue
		}
	}
	if rawJoin, ok := properties["stroke-linejoin"]; ok {
		joinValue := strings.ToLower(strings.TrimSpace(rawJoin))
		if joinValue == "miter" || joinValue == "round" || joinValue == "bevel" {
			state.strokeLinejoin = joinValue
		}
	}
	if includeTransform {
		if rawTransform, ok := properties["transform"]; ok {
			state.transform = state.transform.mul(parseSVGTransformList(rawTransform))
		}
	}
	return state
}

func collectSVGProperties(node *dom.Node) map[string]string {
	props := make(map[string]string)
	if node == nil {
		return props
	}
	for _, attr := range node.Attributes {
		name := strings.ToLower(attr.Name)
		if name == "style" {
			continue
		}
		props[name] = attr.Value
	}
	if inlineStyle, ok := svgAttr(node, "style"); ok {
		for _, decl := range css.ParseDeclarationList(inlineStyle) {
			props[strings.ToLower(decl.Property)] = decl.Value
		}
	}
	return props
}

func renderSVGChildren(dst *image.RGBA, parent *dom.Node, state svgPaintState, styles map[*dom.Node]computedStyle) {
	if parent == nil {
		return
	}
	for _, child := range parent.Children {
		renderSVGElement(dst, child, state, styles)
	}
}

func renderSVGElement(dst *image.RGBA, node *dom.Node, parentState svgPaintState, styles map[*dom.Node]computedStyle) {
	if node == nil || node.Type != dom.ElementNode {
		return
	}
	switch node.Data {
	case "defs", "title", "desc", "metadata", "clipPath", "mask", "linearGradient", "radialGradient":
		return
	}
	if style, ok := styles[node]; ok {
		if style.Display() == displayNone || style.Visibility() != visibilityVisible {
			return
		}
	}

	childColor := parentState.currentColor
	if style, ok := styles[node]; ok {
		childColor = style.Color()
	}
	state := parentState
	state.currentColor = childColor
	state = applySVGElementState(node, state, true)

	switch node.Data {
	case "g", "svg":
		renderSVGChildren(dst, node, state, styles)
		return
	}

	subpaths := svgElementSubpaths(node)
	if len(subpaths) == 0 {
		return
	}

	transformed := make([]svgSubpath, len(subpaths))
	for i, subpath := range subpaths {
		points := make([]svgPoint, len(subpath.points))
		for j, pt := range subpath.points {
			points[j] = state.transform.apply(pt)
		}
		transformed[i] = svgSubpath{points: points, closed: subpath.closed}
	}

	if state.hasFill && state.fill.A > 0 && state.fillOpacity > 0 && node.Data != "line" {
		fillColor := scaleColorAlpha(state.fill, state.fillOpacity*state.opacity)
		rasterizeSVGContours(dst, transformed, fillColor)
	}
	if state.hasStroke && state.stroke.A > 0 && state.strokeOpacity > 0 && state.strokeWidth > 0 {
		strokeColor := scaleColorAlpha(state.stroke, state.strokeOpacity*state.opacity)
		halfWidth := (state.strokeWidth * state.transform.avgScale()) / 2
		if halfWidth < 0.35 {
			halfWidth = 0.35
		}
		var contours []svgSubpath
		for _, subpath := range transformed {
			contours = append(contours, strokeSVGSubpath(subpath, halfWidth, state.strokeLinecap, state.strokeLinejoin)...)
		}
		rasterizeSVGContours(dst, contours, strokeColor)
	}
}

func scaleColorAlpha(c color.NRGBA, multiplier float64) color.NRGBA {
	multiplier = clamp(multiplier, 0, 1)
	c.A = uint8(math.Round(float64(c.A) * multiplier))
	return c
}

func parseSVGPaintColor(raw string, current color.NRGBA) (color.NRGBA, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "none") {
		return color.NRGBA{}, false
	}
	if strings.EqualFold(trimmed, "currentColor") {
		return current, true
	}
	return computed.ParseColor(trimmed, current)
}

func parseSVGFloat(raw string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || !isFinite(value) {
		return 0, false
	}
	return value, true
}

func rasterizeSVGContours(dst *image.RGBA, contours []svgSubpath, paintColor color.NRGBA) {
	if dst == nil || len(contours) == 0 || paintColor.A == 0 {
		return
	}
	bounds := dst.Bounds()
	rast := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
	hasEdges := false
	for _, contour := range contours {
		if len(contour.points) < 2 {
			continue
		}
		hasEdges = true
		rast.MoveTo(float32(contour.points[0].x), float32(contour.points[0].y))
		for i := 1; i < len(contour.points); i++ {
			rast.LineTo(float32(contour.points[i].x), float32(contour.points[i].y))
		}
		rast.ClosePath()
	}
	if !hasEdges {
		return
	}
	rast.Draw(dst, bounds, image.NewUniform(paintColor), image.Point{})
}

func svgElementSubpaths(node *dom.Node) []svgSubpath {
	switch node.Data {
	case "path":
		d, _ := svgAttr(node, "d")
		return parseSVGPath(d)
	case "rect":
		return svgRectSubpaths(node)
	case "circle":
		return svgCircleSubpaths(node)
	case "ellipse":
		return svgEllipseSubpaths(node)
	case "line":
		return svgLineSubpaths(node)
	case "polyline":
		return svgPolySubpaths(node, false)
	case "polygon":
		return svgPolySubpaths(node, true)
	default:
		return nil
	}
}

func svgAttrFloat(node *dom.Node, name string) float64 {
	v, _ := parseSVGLengthAttr(node, name)
	return v
}

func svgAttrSignedFloat(node *dom.Node, name string) float64 {
	raw, ok := svgAttr(node, name)
	if !ok {
		return 0
	}
	v, _ := parseSVGFloat(strings.TrimSuffix(strings.TrimSpace(raw), "px"))
	return v
}

func svgRectSubpaths(node *dom.Node) []svgSubpath {
	x := svgAttrSignedFloat(node, "x")
	y := svgAttrSignedFloat(node, "y")
	w := svgAttrFloat(node, "width")
	h := svgAttrFloat(node, "height")
	if w <= 0 || h <= 0 {
		return nil
	}
	rx := svgAttrFloat(node, "rx")
	ry := svgAttrFloat(node, "ry")
	if rx > 0 && ry == 0 {
		ry = rx
	} else if ry > 0 && rx == 0 {
		rx = ry
	}
	rx = math.Min(rx, w/2)
	ry = math.Min(ry, h/2)
	if rx <= 0 || ry <= 0 {
		return []svgSubpath{{
			points: []svgPoint{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}},
			closed: true,
		}}
	}
	const k = 0.552284749831
	kx := rx * k
	ky := ry * k
	var pts []svgPoint
	pts = append(pts, svgPoint{x + rx, y}, svgPoint{x + w - rx, y})
	pts = appendCubicPoints(pts, svgPoint{x + w - rx, y}, svgPoint{x + w - rx + kx, y}, svgPoint{x + w, y + ry - ky}, svgPoint{x + w, y + ry}, 0)
	pts = append(pts, svgPoint{x + w, y + h - ry})
	pts = appendCubicPoints(pts, svgPoint{x + w, y + h - ry}, svgPoint{x + w, y + h - ry + ky}, svgPoint{x + w - rx + kx, y + h}, svgPoint{x + w - rx, y + h}, 0)
	pts = append(pts, svgPoint{x + rx, y + h})
	pts = appendCubicPoints(pts, svgPoint{x + rx, y + h}, svgPoint{x + rx - kx, y + h}, svgPoint{x, y + h - ry + ky}, svgPoint{x, y + h - ry}, 0)
	pts = append(pts, svgPoint{x, y + ry})
	pts = appendCubicPoints(pts, svgPoint{x, y + ry}, svgPoint{x, y + ry - ky}, svgPoint{x + rx - kx, y}, svgPoint{x + rx, y}, 0)
	return []svgSubpath{{points: pts, closed: true}}
}

func svgCircleSubpaths(node *dom.Node) []svgSubpath {
	cx := svgAttrSignedFloat(node, "cx")
	cy := svgAttrSignedFloat(node, "cy")
	r := svgAttrFloat(node, "r")
	if r <= 0 {
		return nil
	}
	return svgEllipsePoints(cx, cy, r, r)
}

func svgEllipseSubpaths(node *dom.Node) []svgSubpath {
	cx := svgAttrSignedFloat(node, "cx")
	cy := svgAttrSignedFloat(node, "cy")
	rx := svgAttrFloat(node, "rx")
	ry := svgAttrFloat(node, "ry")
	if rx <= 0 || ry <= 0 {
		return nil
	}
	return svgEllipsePoints(cx, cy, rx, ry)
}

func svgEllipsePoints(cx, cy, rx, ry float64) []svgSubpath {
	const k = 0.552284749831
	kx := rx * k
	ky := ry * k
	top := svgPoint{cx, cy - ry}
	right := svgPoint{cx + rx, cy}
	bottom := svgPoint{cx, cy + ry}
	left := svgPoint{cx - rx, cy}
	pts := []svgPoint{top}
	pts = appendCubicPoints(pts, top, svgPoint{cx + kx, cy - ry}, svgPoint{cx + rx, cy - ky}, right, 0)
	pts = appendCubicPoints(pts, right, svgPoint{cx + rx, cy + ky}, svgPoint{cx + kx, cy + ry}, bottom, 0)
	pts = appendCubicPoints(pts, bottom, svgPoint{cx - kx, cy + ry}, svgPoint{cx - rx, cy + ky}, left, 0)
	pts = appendCubicPoints(pts, left, svgPoint{cx - rx, cy - ky}, svgPoint{cx - kx, cy - ry}, top, 0)
	return []svgSubpath{{points: pts, closed: true}}
}

func svgLineSubpaths(node *dom.Node) []svgSubpath {
	x1 := svgAttrSignedFloat(node, "x1")
	y1 := svgAttrSignedFloat(node, "y1")
	x2 := svgAttrSignedFloat(node, "x2")
	y2 := svgAttrSignedFloat(node, "y2")
	return []svgSubpath{{points: []svgPoint{{x1, y1}, {x2, y2}}, closed: false}}
}

func svgPolySubpaths(node *dom.Node, closed bool) []svgSubpath {
	raw, ok := svgAttr(node, "points")
	if !ok {
		return nil
	}
	nums := parseSVGNumberList(raw)
	if len(nums) < 4 {
		return nil
	}
	pts := make([]svgPoint, 0, len(nums)/2)
	for i := 0; i+1 < len(nums); i += 2 {
		pts = append(pts, svgPoint{x: nums[i], y: nums[i+1]})
	}
	return []svgSubpath{{points: pts, closed: closed}}
}

func parseSVGNumberList(raw string) []float64 {
	var numbers []float64
	i := 0
	for i < len(raw) {
		for i < len(raw) && (raw[i] == ',' || unicode.IsSpace(rune(raw[i]))) {
			i++
		}
		if i >= len(raw) {
			break
		}
		num, next, ok := scanSVGNumber(raw, i)
		if !ok {
			i++
			continue
		}
		numbers = append(numbers, num)
		i = next
	}
	return numbers
}

func scanSVGNumber(src string, start int) (float64, int, bool) {
	i := start
	n := len(src)
	if i < n && (src[i] == '+' || src[i] == '-') {
		i++
	}
	digits := 0
	for i < n && src[i] >= '0' && src[i] <= '9' {
		i++
		digits++
	}
	if i < n && src[i] == '.' {
		i++
		for i < n && src[i] >= '0' && src[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0, start, false
	}
	if i < n && (src[i] == 'e' || src[i] == 'E') {
		expPos := i
		i++
		if i < n && (src[i] == '+' || src[i] == '-') {
			i++
		}
		expDigits := 0
		for i < n && src[i] >= '0' && src[i] <= '9' {
			i++
			expDigits++
		}
		if expDigits == 0 {
			i = expPos
		}
	}
	val, err := strconv.ParseFloat(src[start:i], 64)
	if err != nil || !isFinite(val) {
		return 0, start, false
	}
	return val, i, true
}

func parseSVGPath(d string) []svgSubpath {
	var subpaths []svgSubpath
	var current svgSubpath
	hasCurrent := false
	flushSubpath := func() {
		if hasCurrent && len(current.points) > 0 {
			subpaths = append(subpaths, current)
		}
		current = svgSubpath{}
		hasCurrent = false
	}

	i := 0
	n := len(d)
	skipSep := func() {
		for i < n && (d[i] == ',' || unicode.IsSpace(rune(d[i]))) {
			i++
		}
	}
	readNum := func() (float64, bool) {
		skipSep()
		if i >= n {
			return 0, false
		}
		val, next, ok := scanSVGNumber(d, i)
		if !ok {
			return 0, false
		}
		i = next
		return val, true
	}
	readFlag := func() (bool, bool) {
		skipSep()
		if i >= n {
			return false, false
		}
		if d[i] == '0' {
			i++
			return false, true
		}
		if d[i] == '1' {
			i++
			return true, true
		}
		return false, false
	}

	var cmd byte
	curPt := svgPoint{}
	startPt := svgPoint{}
	lastCtrl := svgPoint{}
	var prevCmd byte

	for {
		skipSep()
		if i >= n {
			break
		}
		if (d[i] >= 'A' && d[i] <= 'Z') || (d[i] >= 'a' && d[i] <= 'z') {
			cmd = d[i]
			i++
		} else if cmd == 0 {
			break
		}

		rel := cmd >= 'a' && cmd <= 'z'
		upper := cmd
		if rel {
			upper -= 'a' - 'A'
		}

		switch upper {
		case 'M':
			x, okX := readNum()
			y, okY := readNum()
			if !okX || !okY {
				i = n
				break
			}
			flushSubpath()
			if rel {
				x += curPt.x
				y += curPt.y
			}
			curPt = svgPoint{x, y}
			startPt = curPt
			current = svgSubpath{points: []svgPoint{curPt}}
			hasCurrent = true
			lastCtrl = curPt
			prevCmd = upper
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}

		case 'L':
			x, okX := readNum()
			y, okY := readNum()
			if !okX || !okY {
				i = n
				break
			}
			if rel {
				x += curPt.x
				y += curPt.y
			}
			curPt = svgPoint{x, y}
			current.points = append(current.points, curPt)
			lastCtrl = curPt
			prevCmd = upper

		case 'H':
			x, ok := readNum()
			if !ok {
				i = n
				break
			}
			if rel {
				x += curPt.x
			}
			curPt.x = x
			current.points = append(current.points, curPt)
			lastCtrl = curPt
			prevCmd = upper

		case 'V':
			y, ok := readNum()
			if !ok {
				i = n
				break
			}
			if rel {
				y += curPt.y
			}
			curPt.y = y
			current.points = append(current.points, curPt)
			lastCtrl = curPt
			prevCmd = upper

		case 'C':
			x1, ok1 := readNum()
			y1, ok2 := readNum()
			x2, ok3 := readNum()
			y2, ok4 := readNum()
			x, ok5 := readNum()
			y, ok6 := readNum()
			if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
				i = n
				break
			}
			if rel {
				x1 += curPt.x
				y1 += curPt.y
				x2 += curPt.x
				y2 += curPt.y
				x += curPt.x
				y += curPt.y
			}
			c1, c2, end := svgPoint{x1, y1}, svgPoint{x2, y2}, svgPoint{x, y}
			current.points = appendCubicPoints(current.points, curPt, c1, c2, end, 0)
			curPt = end
			lastCtrl = c2
			prevCmd = upper

		case 'S':
			x2, ok1 := readNum()
			y2, ok2 := readNum()
			x, ok3 := readNum()
			y, ok4 := readNum()
			if !ok1 || !ok2 || !ok3 || !ok4 {
				i = n
				break
			}
			c1 := curPt
			if prevCmd == 'C' || prevCmd == 'S' {
				c1 = svgPoint{2*curPt.x - lastCtrl.x, 2*curPt.y - lastCtrl.y}
			}
			if rel {
				x2 += curPt.x
				y2 += curPt.y
				x += curPt.x
				y += curPt.y
			}
			c2, end := svgPoint{x2, y2}, svgPoint{x, y}
			current.points = appendCubicPoints(current.points, curPt, c1, c2, end, 0)
			curPt = end
			lastCtrl = c2
			prevCmd = upper

		case 'Q':
			x1, ok1 := readNum()
			y1, ok2 := readNum()
			x, ok3 := readNum()
			y, ok4 := readNum()
			if !ok1 || !ok2 || !ok3 || !ok4 {
				i = n
				break
			}
			if rel {
				x1 += curPt.x
				y1 += curPt.y
				x += curPt.x
				y += curPt.y
			}
			ctrl, end := svgPoint{x1, y1}, svgPoint{x, y}
			current.points = appendQuadraticPoints(current.points, curPt, ctrl, end, 0)
			curPt = end
			lastCtrl = ctrl
			prevCmd = upper

		case 'T':
			x, ok1 := readNum()
			y, ok2 := readNum()
			if !ok1 || !ok2 {
				i = n
				break
			}
			ctrl := curPt
			if prevCmd == 'Q' || prevCmd == 'T' {
				ctrl = svgPoint{2*curPt.x - lastCtrl.x, 2*curPt.y - lastCtrl.y}
			}
			if rel {
				x += curPt.x
				y += curPt.y
			}
			end := svgPoint{x, y}
			current.points = appendQuadraticPoints(current.points, curPt, ctrl, end, 0)
			curPt = end
			lastCtrl = ctrl
			prevCmd = upper

		case 'A':
			rx, ok1 := readNum()
			ry, ok2 := readNum()
			xRot, ok3 := readNum()
			largeArc, ok4 := readFlag()
			sweep, ok5 := readFlag()
			x, ok6 := readNum()
			y, ok7 := readNum()
			if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !ok7 {
				i = n
				break
			}
			if rel {
				x += curPt.x
				y += curPt.y
			}
			end := svgPoint{x, y}
			current.points = appendArcPoints(current.points, curPt, rx, ry, xRot, largeArc, sweep, end)
			curPt = end
			lastCtrl = curPt
			prevCmd = upper

		case 'Z':
			if hasCurrent {
				current.closed = true
				flushSubpath()
			}
			curPt = startPt
			lastCtrl = curPt
			prevCmd = upper
			cmd = 0

		default:
			i = n
		}
	}
	flushSubpath()
	return subpaths
}

func appendCubicPoints(dst []svgPoint, p0, p1, p2, p3 svgPoint, depth int) []svgPoint {
	d1 := pointLineDist(p1, p0, p3)
	d2 := pointLineDist(p2, p0, p3)
	if math.Max(d1, d2) <= svgFlattenTol/2 || depth >= svgMaxSubdivDepth {
		return append(dst, p3)
	}
	p01 := midpoint(p0, p1)
	p12 := midpoint(p1, p2)
	p23 := midpoint(p2, p3)
	p012 := midpoint(p01, p12)
	p123 := midpoint(p12, p23)
	p0123 := midpoint(p012, p123)
	dst = appendCubicPoints(dst, p0, p01, p012, p0123, depth+1)
	return appendCubicPoints(dst, p0123, p123, p23, p3, depth+1)
}

func appendQuadraticPoints(dst []svgPoint, p0, p1, p2 svgPoint, depth int) []svgPoint {
	c1 := svgPoint{p0.x + (p1.x-p0.x)*2/3, p0.y + (p1.y-p0.y)*2/3}
	c2 := svgPoint{p2.x + (p1.x-p2.x)*2/3, p2.y + (p1.y-p2.y)*2/3}
	return appendCubicPoints(dst, p0, c1, c2, p2, depth)
}

func appendArcPoints(dst []svgPoint, p0 svgPoint, rx, ry, xAxisRotDeg float64, largeArc, sweep bool, p1 svgPoint) []svgPoint {
	if p0 == p1 {
		return dst
	}
	if rx == 0 || ry == 0 {
		return append(dst, p1)
	}
	rx = math.Abs(rx)
	ry = math.Abs(ry)
	phi := xAxisRotDeg * math.Pi / 180
	cosPhi, sinPhi := math.Cos(phi), math.Sin(phi)
	dx2 := (p0.x - p1.x) / 2
	dy2 := (p0.y - p1.y) / 2
	x1p := cosPhi*dx2 + sinPhi*dy2
	y1p := -sinPhi*dx2 + cosPhi*dy2

	rxSq := rx * rx
	rySq := ry * ry
	x1pSq := x1p * x1p
	y1pSq := y1p * y1p
	lambda := x1pSq/rxSq + y1pSq/rySq
	if lambda > 1 {
		scale := math.Sqrt(lambda)
		rx *= scale
		ry *= scale
		rxSq = rx * rx
		rySq = ry * ry
	}

	num := math.Max(0, rxSq*rySq-rxSq*y1pSq-rySq*x1pSq)
	den := rxSq*y1pSq + rySq*x1pSq
	factor := 0.0
	if den > 0 {
		factor = math.Sqrt(num / den)
	}
	if largeArc == sweep {
		factor = -factor
	}
	cxp := factor * rx * y1p / ry
	cyp := -factor * ry * x1p / rx
	cx := cosPhi*cxp - sinPhi*cyp + (p0.x+p1.x)/2
	cy := sinPhi*cxp + cosPhi*cyp + (p0.y+p1.y)/2

	angle := func(ux, uy, vx, vy float64) float64 {
		dot := ux*vx + uy*vy
		lenProd := math.Hypot(ux, uy) * math.Hypot(vx, vy)
		if lenProd == 0 {
			return 0
		}
		cosVal := clamp(dot/lenProd, -1, 1)
		a := math.Acos(cosVal)
		if ux*vy-uy*vx < 0 {
			a = -a
		}
		return a
	}

	theta1 := angle(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dTheta := angle((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dTheta > 0 {
		dTheta -= 2 * math.Pi
	} else if sweep && dTheta < 0 {
		dTheta += 2 * math.Pi
	}

	segments := max(1, int(math.Ceil(math.Abs(dTheta)/(math.Pi/12))))
	for step := 1; step <= segments; step++ {
		t := theta1 + dTheta*float64(step)/float64(segments)
		px := rx * math.Cos(t)
		py := ry * math.Sin(t)
		dst = append(dst, svgPoint{
			x: cosPhi*px - sinPhi*py + cx,
			y: sinPhi*px + cosPhi*py + cy,
		})
	}
	return dst
}

func midpoint(a, b svgPoint) svgPoint {
	return svgPoint{(a.x + b.x) / 2, (a.y + b.y) / 2}
}

func pointLineDist(p, a, b svgPoint) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	length := math.Hypot(dx, dy)
	if length == 0 {
		return math.Hypot(p.x-a.x, p.y-a.y)
	}
	return math.Abs((p.x-a.x)*dy-(p.y-a.y)*dx) / length
}

func strokeSVGSubpath(subpath svgSubpath, hw float64, linecap, linejoin string) []svgSubpath {
	pts := dedupePoints(subpath.points, subpath.closed)
	if len(pts) < 2 {
		return nil
	}
	n := len(pts)
	segCount := n - 1
	if subpath.closed {
		segCount = n
	}
	var contours []svgSubpath
	for i := 0; i < segCount; i++ {
		a := pts[i]
		b := pts[(i+1)%n]
		dx, dy := b.x-a.x, b.y-a.y
		length := math.Hypot(dx, dy)
		if length < 1e-4 {
			continue
		}
		ux, uy := dx/length, dy/length
		nx, ny := -uy*hw, ux*hw
		p0, p1 := a, b
		if !subpath.closed && linecap == "square" {
			if i == 0 {
				p0 = svgPoint{a.x - ux*hw, a.y - uy*hw}
			}
			if i == segCount-1 {
				p1 = svgPoint{b.x + ux*hw, b.y + uy*hw}
			}
		}
		contours = append(contours, svgSubpath{
			points: []svgPoint{
				{p0.x + nx, p0.y + ny},
				{p1.x + nx, p1.y + ny},
				{p1.x - nx, p1.y - ny},
				{p0.x - nx, p0.y - ny},
			},
			closed: true,
		})
	}
	if !subpath.closed && linecap == "round" {
		contours = append(contours, strokeCircleCap(pts[0], hw), strokeCircleCap(pts[len(pts)-1], hw))
	}
	if hw > 0.5 {
		joinStart, joinEnd := 1, n-1
		if subpath.closed {
			joinStart, joinEnd = 0, n
		}
		for i := joinStart; i < joinEnd; i++ {
			contours = append(contours, strokeCircleCap(pts[i], hw))
		}
	}
	return contours
}

func strokeCircleCap(center svgPoint, radius float64) svgSubpath {
	const steps = 12
	pts := make([]svgPoint, steps)
	for i := 0; i < steps; i++ {
		theta := 2 * math.Pi * float64(i) / steps
		pts[i] = svgPoint{center.x + radius*math.Cos(theta), center.y + radius*math.Sin(theta)}
	}
	return svgSubpath{points: pts, closed: true}
}

func dedupePoints(pts []svgPoint, closed bool) []svgPoint {
	out := make([]svgPoint, 0, len(pts))
	for _, pt := range pts {
		if len(out) == 0 || math.Hypot(pt.x-out[len(out)-1].x, pt.y-out[len(out)-1].y) > 1e-4 {
			out = append(out, pt)
		}
	}
	if closed && len(out) > 1 && math.Hypot(out[0].x-out[len(out)-1].x, out[0].y-out[len(out)-1].y) <= 1e-4 {
		out = out[:len(out)-1]
	}
	return out
}

func parseSVGTransformList(raw string) svgMatrix {
	matrix := svgIdentityMatrix()
	src := raw
	for len(src) > 0 {
		openIdx := strings.IndexByte(src, '(')
		if openIdx < 0 {
			break
		}
		closeIdx := strings.IndexByte(src[openIdx:], ')')
		if closeIdx < 0 {
			break
		}
		name := strings.ToLower(strings.TrimSpace(src[:openIdx]))
		name = strings.TrimLeft(name, ", \t\n\r")
		args := parseSVGNumberList(src[openIdx+1 : openIdx+closeIdx])
		switch name {
		case "translate":
			if len(args) >= 1 {
				ty := 0.0
				if len(args) >= 2 {
					ty = args[1]
				}
				matrix = matrix.mul(svgMatrix{a: 1, d: 1, e: args[0], f: ty})
			}
		case "scale":
			if len(args) >= 1 {
				sy := args[0]
				if len(args) >= 2 {
					sy = args[1]
				}
				matrix = matrix.mul(svgMatrix{a: args[0], d: sy})
			}
		case "rotate":
			if len(args) >= 1 {
				rad := args[0] * math.Pi / 180
				cosV, sinV := math.Cos(rad), math.Sin(rad)
				rot := svgMatrix{a: cosV, b: sinV, c: -sinV, d: cosV}
				if len(args) >= 3 {
					rot = svgMatrix{a: 1, d: 1, e: args[1], f: args[2]}.mul(rot).mul(svgMatrix{a: 1, d: 1, e: -args[1], f: -args[2]})
				}
				matrix = matrix.mul(rot)
			}
		case "matrix":
			if len(args) == 6 {
				matrix = matrix.mul(svgMatrix{a: args[0], b: args[1], c: args[2], d: args[3], e: args[4], f: args[5]})
			}
		}
		src = src[openIdx+closeIdx+1:]
	}
	return matrix
}
