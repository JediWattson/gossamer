package window

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var graphitePalette = struct {
	ink, top, surface, raised, border, pearl, muted, dim color.NRGBA
	teal, tealDim, violet, danger                        color.NRGBA
}{
	ink:     color.NRGBA{R: 0x08, G: 0x0c, B: 0x11, A: 0xff},
	top:     color.NRGBA{R: 0x0c, G: 0x12, B: 0x19, A: 0xff},
	surface: color.NRGBA{R: 0x11, G: 0x19, B: 0x22, A: 0xff},
	raised:  color.NRGBA{R: 0x18, G: 0x22, B: 0x2c, A: 0xff},
	border:  color.NRGBA{R: 0x32, G: 0x40, B: 0x4d, A: 0xff},
	pearl:   color.NRGBA{R: 0xed, G: 0xf2, B: 0xf4, A: 0xff},
	muted:   color.NRGBA{R: 0x9b, G: 0xa7, B: 0xb0, A: 0xff},
	dim:     color.NRGBA{R: 0x58, G: 0x65, B: 0x70, A: 0xff},
	teal:    color.NRGBA{R: 0x46, G: 0xd8, B: 0xd0, A: 0xff},
	tealDim: color.NRGBA{R: 0x1b, G: 0x70, B: 0x70, A: 0xff},
	violet:  color.NRGBA{R: 0xb6, G: 0x8c, B: 0xff, A: 0xff},
	danger:  color.NRGBA{R: 0xff, G: 0x76, B: 0x83, A: 0xff},
}

type shellFaceKey struct {
	size int
	bold bool
}

type shellFontBook struct {
	regular *opentype.Font
	bold    *opentype.Font
	faces   map[shellFaceKey]font.Face
}

func newShellFontBook() (*shellFontBook, error) {
	regular, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("window: parse Graphite regular font: %w", err)
	}
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, fmt.Errorf("window: parse Graphite bold font: %w", err)
	}
	return &shellFontBook{regular: regular, bold: bold, faces: make(map[shellFaceKey]font.Face)}, nil
}

func (book *shellFontBook) close() error {
	if book == nil {
		return nil
	}
	var result error
	for _, face := range book.faces {
		closer, ok := face.(io.Closer)
		if ok {
			if err := closer.Close(); err != nil && result == nil {
				result = err
			}
		}
	}
	book.faces = nil
	return result
}

func (book *shellFontBook) face(size float64, bold bool) (font.Face, error) {
	key := shellFaceKey{size: int(math.Round(size * 64)), bold: bold}
	if existing := book.faces[key]; existing != nil {
		return existing, nil
	}
	parsed := book.regular
	if bold {
		parsed = book.bold
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("window: create Graphite font face: %w", err)
	}
	book.faces[key] = face
	return face, nil
}

func (book *shellFontBook) draw(dst *image.RGBA, clip image.Rectangle, text string, x, baseline, size int, textColor color.NRGBA, bold bool) error {
	face, err := book.face(float64(size), bold)
	if err != nil {
		return err
	}
	if clip.Empty() {
		return nil
	}
	clipped, ok := dst.SubImage(clip.Intersect(dst.Bounds())).(*image.RGBA)
	if !ok {
		return fmt.Errorf("window: Graphite text destination is not RGBA")
	}
	drawer := font.Drawer{
		Dst:  clipped,
		Src:  image.NewUniform(textColor),
		Face: face,
		Dot:  fixed.P(x, baseline),
	}
	drawer.DrawString(text)
	return nil
}

func (shell *graphiteShell) compose(pageCanvas *image.RGBA, page *browser.Page) (*image.RGBA, error) {
	if shell == nil || shell.fonts == nil {
		return nil, fmt.Errorf("window: nil Graphite shell")
	}
	if shell.width <= 0 || shell.height <= 0 {
		return nil, fmt.Errorf("window: invalid Graphite dimensions %dx%d", shell.width, shell.height)
	}
	layout := shell.layout()
	canvas := image.NewRGBA(layout.window)
	fillRect(canvas, layout.window, graphitePalette.ink)
	if pageCanvas != nil {
		draw.Draw(canvas, layout.content, pageCanvas, pageCanvas.Bounds().Min, draw.Src)
	}

	fillRect(canvas, image.Rect(0, 0, layout.rail.Min.X, graphiteTabHeight), graphitePalette.top)
	fillRect(canvas, image.Rect(0, graphiteTabHeight, layout.rail.Min.X, graphiteChromeHeight), graphitePalette.surface)
	fillRect(canvas, layout.rail, graphitePalette.top)
	fillRect(canvas, image.Rect(0, graphiteTabHeight-1, layout.rail.Min.X, graphiteTabHeight), graphitePalette.border)
	fillRect(canvas, image.Rect(0, graphiteChromeHeight-1, layout.rail.Min.X, graphiteChromeHeight), graphitePalette.border)
	fillRect(canvas, image.Rect(layout.rail.Min.X, 0, layout.rail.Min.X+1, shell.height), graphitePalette.border)

	if err := shell.drawTab(canvas, layout, page); err != nil {
		return nil, err
	}
	if err := shell.drawToolbar(canvas, layout); err != nil {
		return nil, err
	}
	if err := shell.drawRail(canvas, layout, page); err != nil {
		return nil, err
	}
	if shell.loading {
		lineRight := layout.content.Max.X
		if lineRight > 180 {
			lineRight = 180
		}
		fillRect(canvas, image.Rect(0, graphiteChromeHeight-2, lineRight, graphiteChromeHeight), graphitePalette.teal)
	}
	return canvas, nil
}

func (shell *graphiteShell) drawTab(canvas *image.RGBA, layout shellLayout, page *browser.Page) error {
	if layout.tab.Empty() {
		return nil
	}
	fillTopRoundedRect(canvas, layout.tab, graphiteTabTopRadius, graphitePalette.surface)
	fillRect(canvas, image.Rect(layout.tab.Min.X, layout.tab.Max.Y-1, layout.tab.Max.X, layout.tab.Max.Y), graphitePalette.tealDim)
	drawKnot(canvas, image.Pt(layout.tab.Min.X+16, layout.tab.Min.Y+14), 8)
	titleClip := image.Rect(layout.tab.Min.X+34, layout.tab.Min.Y, layout.tabClose.Min.X-4, layout.tab.Max.Y)
	if err := shell.fonts.draw(canvas, titleClip, shellTabTitle(page), titleClip.Min.X, layout.tab.Min.Y+20, 12, graphitePalette.pearl, true); err != nil {
		return err
	}
	drawClose(canvas, layout.tabClose, graphitePalette.muted)
	return nil
}

func (shell *graphiteShell) drawToolbar(canvas *image.RGBA, layout shellLayout) error {
	drawChevron(canvas, center(layout.back), false, graphitePalette.dim)
	drawChevron(canvas, center(layout.forward), true, graphitePalette.dim)
	drawReload(canvas, center(layout.reload), graphitePalette.muted)

	addressFill := graphitePalette.ink
	addressBorder := graphitePalette.border
	if shell.addressFocused {
		addressBorder = graphitePalette.teal
	}
	if shell.navigationErr != "" {
		addressBorder = graphitePalette.danger
	}
	fillRoundedRect(canvas, layout.address, graphiteAddressRadius, addressFill)
	strokeRoundedRect(canvas, layout.address, graphiteAddressRadius, 1, addressBorder)
	drawShield(canvas, image.Pt(layout.address.Min.X+17, center(layout.address).Y), graphitePalette.teal)
	addressClip := image.Rect(layout.address.Min.X+33, layout.address.Min.Y+2, layout.address.Max.X-12, layout.address.Max.Y-2)
	addressText := shell.address
	textColor := graphitePalette.pearl
	if addressText == "" {
		addressText = "Search or enter address"
		textColor = graphitePalette.dim
	}
	if err := shell.fonts.draw(canvas, addressClip, addressText, addressClip.Min.X, layout.address.Min.Y+22, 13, textColor, false); err != nil {
		return err
	}
	if shell.addressFocused && shell.selectAll {
		fillRectAlpha(canvas, image.Rect(addressClip.Min.X-2, addressClip.Min.Y+4, addressClip.Max.X, addressClip.Max.Y-4), color.NRGBA{R: 0x2b, G: 0x79, B: 0x7b, A: 0x78})
		if err := shell.fonts.draw(canvas, addressClip, addressText, addressClip.Min.X, layout.address.Min.Y+22, 13, graphitePalette.pearl, false); err != nil {
			return err
		}
	}
	return nil
}

func (shell *graphiteShell) drawRail(canvas *image.RGBA, layout shellLayout, page *browser.Page) error {
	centerX := center(layout.rail).X
	drawKnot(canvas, image.Pt(centerX, 22), 9)
	drawRailGlyph(canvas, image.Pt(centerX, 72), "nodes", graphitePalette.teal)
	drawRailGlyph(canvas, image.Pt(centerX, 120), "list", graphitePalette.muted)
	drawRailGlyph(canvas, image.Pt(centerX, 168), "pulse", graphitePalette.violet)
	disclosureColor := graphitePalette.muted
	if shell.inspectorOpen {
		disclosureColor = graphitePalette.teal
		fillRect(canvas, image.Rect(layout.rail.Min.X, layout.railDisclosure.Min.Y, layout.rail.Min.X+2, layout.railDisclosure.Max.Y), graphitePalette.teal)
	}
	drawChevron(canvas, center(layout.railDisclosure), !shell.inspectorOpen, disclosureColor)
	drawGear(canvas, image.Pt(centerX, shell.height-24), graphitePalette.dim)
	if shell.inspectorOpen {
		return shell.drawInspector(canvas, layout, page)
	}
	return nil
}

func (shell *graphiteShell) drawInspector(canvas *image.RGBA, layout shellLayout, page *browser.Page) error {
	panel := layout.inspector
	if panel.Empty() {
		return nil
	}
	fillRectAlpha(canvas, panel, color.NRGBA{R: 0x0c, G: 0x12, B: 0x19, A: 0xf6})
	fillRect(canvas, image.Rect(panel.Min.X, panel.Min.Y, panel.Min.X+1, panel.Max.Y), graphitePalette.border)
	content := panel.Inset(18)
	content.Max.Y = panel.Max.Y - 12
	y := panel.Min.Y + 28
	drawKnot(canvas, image.Pt(content.Min.X+10, y-5), 10)
	if err := shell.fonts.draw(canvas, content, "GOSSAMER", content.Min.X+32, y, 13, graphitePalette.pearl, true); err != nil {
		return err
	}
	y += 34
	if err := shell.drawInspectorLabel(canvas, content, &y, "ENGINE SOCKET", graphitePalette.teal); err != nil {
		return err
	}
	if err := shell.drawInspectorValue(canvas, content, &y, "Active", "V8 reference"); err != nil {
		return err
	}
	if err := shell.drawInspectorValue(canvas, content, &y, "Target", "Strand"); err != nil {
		return err
	}
	y += 12
	if err := shell.drawInspectorLabel(canvas, content, &y, "GO KERNEL", graphitePalette.violet); err != nil {
		return err
	}
	profile := page.Realm.Profile()
	navigation := page.Navigation()
	values := [][2]string{
		{"Navigation", shellNavigationLabel(navigation)},
		{"Regions", fmt.Sprint(profile.Memory.LiveRegions)},
		{"Native objects", fmt.Sprint(profile.Memory.LiveHostObjects)},
		{"Ownership claims", fmt.Sprint(profile.Ownership.LiveObjects)},
		{"Queue depth", fmt.Sprint(profile.TaskDepth + profile.MicrotaskDepth)},
	}
	for _, value := range values {
		if err := shell.drawInspectorValue(canvas, content, &y, value[0], value[1]); err != nil {
			return err
		}
	}
	if shell.navigationErr != "" {
		y += 10
		if err := shell.fonts.draw(canvas, content, "NAVIGATION ERROR", content.Min.X, y, 10, graphitePalette.danger, true); err != nil {
			return err
		}
		y += 18
		message := shell.navigationErr
		if len(message) > 64 {
			message = message[:61] + "..."
		}
		return shell.fonts.draw(canvas, content, message, content.Min.X, y, 11, graphitePalette.muted, false)
	}
	return nil
}

func (shell *graphiteShell) drawInspectorLabel(canvas *image.RGBA, clip image.Rectangle, y *int, label string, labelColor color.NRGBA) error {
	if err := shell.fonts.draw(canvas, clip, label, clip.Min.X, *y, 10, labelColor, true); err != nil {
		return err
	}
	*y += 24
	return nil
}

func (shell *graphiteShell) drawInspectorValue(canvas *image.RGBA, clip image.Rectangle, y *int, label, value string) error {
	if err := shell.fonts.draw(canvas, clip, label, clip.Min.X, *y, 11, graphitePalette.muted, false); err != nil {
		return err
	}
	valueX := clip.Min.X + 112
	if err := shell.fonts.draw(canvas, clip, value, valueX, *y, 11, graphitePalette.pearl, false); err != nil {
		return err
	}
	*y += 22
	return nil
}

func fillRect(dst *image.RGBA, rectangle image.Rectangle, fill color.NRGBA) {
	draw.Draw(dst, rectangle.Intersect(dst.Bounds()), image.NewUniform(fill), image.Point{}, draw.Src)
}

func fillRectAlpha(dst *image.RGBA, rectangle image.Rectangle, fill color.NRGBA) {
	draw.Draw(dst, rectangle.Intersect(dst.Bounds()), image.NewUniform(fill), image.Point{}, draw.Over)
}

func fillRoundedRect(dst *image.RGBA, rectangle image.Rectangle, radius int, fill color.NRGBA) {
	rectangle = rectangle.Intersect(dst.Bounds())
	if rectangle.Empty() {
		return
	}
	if radius <= 0 {
		fillRect(dst, rectangle, fill)
		return
	}
	maximum := minInt(rectangle.Dx()/2, rectangle.Dy()/2)
	if radius > maximum {
		radius = maximum
	}
	fillRect(dst, image.Rect(rectangle.Min.X+radius, rectangle.Min.Y, rectangle.Max.X-radius, rectangle.Max.Y), fill)
	fillRect(dst, image.Rect(rectangle.Min.X, rectangle.Min.Y+radius, rectangle.Max.X, rectangle.Max.Y-radius), fill)
	for y := 0; y < radius; y++ {
		for x := 0; x < radius; x++ {
			dx := float64(radius-x) - 0.5
			dy := float64(radius-y) - 0.5
			if dx*dx+dy*dy > float64(radius*radius) {
				continue
			}
			setNRGBA(dst, rectangle.Min.X+x, rectangle.Min.Y+y, fill)
			setNRGBA(dst, rectangle.Max.X-1-x, rectangle.Min.Y+y, fill)
			setNRGBA(dst, rectangle.Min.X+x, rectangle.Max.Y-1-y, fill)
			setNRGBA(dst, rectangle.Max.X-1-x, rectangle.Max.Y-1-y, fill)
		}
	}
}

func fillTopRoundedRect(dst *image.RGBA, rectangle image.Rectangle, radius int, fill color.NRGBA) {
	rectangle = rectangle.Intersect(dst.Bounds())
	if rectangle.Empty() {
		return
	}
	if radius <= 0 {
		fillRect(dst, rectangle, fill)
		return
	}
	maximum := minInt(rectangle.Dx()/2, rectangle.Dy())
	if radius > maximum {
		radius = maximum
	}
	fillRect(dst, image.Rect(rectangle.Min.X+radius, rectangle.Min.Y, rectangle.Max.X-radius, rectangle.Max.Y), fill)
	fillRect(dst, image.Rect(rectangle.Min.X, rectangle.Min.Y+radius, rectangle.Max.X, rectangle.Max.Y), fill)
	for y := 0; y < radius; y++ {
		for x := 0; x < radius; x++ {
			dx := float64(radius-x) - 0.5
			dy := float64(radius-y) - 0.5
			if dx*dx+dy*dy > float64(radius*radius) {
				continue
			}
			setNRGBA(dst, rectangle.Min.X+x, rectangle.Min.Y+y, fill)
			setNRGBA(dst, rectangle.Max.X-1-x, rectangle.Min.Y+y, fill)
		}
	}
}

func strokeRoundedRect(dst *image.RGBA, rectangle image.Rectangle, radius, width int, stroke color.NRGBA) {
	fillRoundedRect(dst, rectangle, radius, stroke)
	inner := rectangle.Inset(width)
	if !inner.Empty() {
		fillRoundedRect(dst, inner, maxInt(0, radius-width), graphitePalette.ink)
	}
}

func setNRGBA(dst *image.RGBA, x, y int, value color.NRGBA) {
	if image.Pt(x, y).In(dst.Bounds()) {
		dst.Set(x, y, value)
	}
}

func center(rectangle image.Rectangle) image.Point {
	return image.Pt((rectangle.Min.X+rectangle.Max.X)/2, (rectangle.Min.Y+rectangle.Max.Y)/2)
}

func drawThickLine(dst *image.RGBA, from, to image.Point, width int, stroke color.NRGBA) {
	dx := to.X - from.X
	dy := to.Y - from.Y
	steps := maxInt(absInt(dx), absInt(dy))
	if steps == 0 {
		fillCircle(dst, from, width/2, stroke)
		return
	}
	for step := 0; step <= steps; step++ {
		x := from.X + dx*step/steps
		y := from.Y + dy*step/steps
		fillCircle(dst, image.Pt(x, y), maxInt(1, width/2), stroke)
	}
}

func fillCircle(dst *image.RGBA, center image.Point, radius int, fill color.NRGBA) {
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y <= radius*radius {
				setNRGBA(dst, center.X+x, center.Y+y, fill)
			}
		}
	}
}

func drawKnot(dst *image.RGBA, center image.Point, radius int) {
	crossing := knotCrossingOffset(radius)
	width := maxInt(2, radius/3)
	gapWidth := width + 2
	gap := graphitePalette.ink
	tealPath := knotCapsulePath(center, radius, false)
	violetPath := knotCapsulePath(center, radius, true)

	// Paint both closed strands, then restore the teal bridge at the north and
	// south crossings. Violet remains above at east and west, producing a real
	// alternating over-under knot rather than two adjacent chain links.
	drawPolyline(dst, tealPath, gapWidth, gap)
	drawPolyline(dst, tealPath, width, graphitePalette.teal)
	drawPolyline(dst, violetPath, gapWidth, gap)
	drawPolyline(dst, violetPath, width, graphitePalette.violet)
	bridge := maxInt(2, width)
	for _, y := range []int{-crossing, crossing} {
		from := center.Add(image.Pt(-bridge, y-bridge))
		to := center.Add(image.Pt(bridge, y+bridge))
		drawThickLine(dst, from, to, gapWidth, gap)
		drawThickLine(dst, from, to, width, graphitePalette.teal)
	}
	pearlRadius := maxInt(1, radius/8)
	for _, offset := range []image.Point{
		image.Pt(0, -crossing), image.Pt(crossing, 0),
		image.Pt(0, crossing), image.Pt(-crossing, 0),
	} {
		fillCircle(dst, center.Add(offset), pearlRadius, graphitePalette.pearl)
	}
}

func knotCrossingOffset(radius int) int {
	return maxInt(2, int(math.Round(knotCapsuleHalfWidth(radius)*math.Sqrt2)))
}

func knotCapsuleHalfWidth(radius int) float64 {
	return math.Max(2, float64(radius)*0.42)
}

func knotCapsulePath(center image.Point, radius int, opposite bool) []image.Point {
	const diagonal = math.Sqrt2 / 2
	directionX := diagonal
	directionY := diagonal
	if opposite {
		directionX = -diagonal
	}
	normalX := -directionY
	normalY := directionX
	halfWidth := knotCapsuleHalfWidth(radius)
	halfLength := math.Max(1, float64(radius)*math.Sqrt2-halfWidth)
	centerX := float64(center.X)
	centerY := float64(center.Y)
	startX := centerX - directionX*halfLength
	startY := centerY - directionY*halfLength
	endX := centerX + directionX*halfLength
	endY := centerY + directionY*halfLength
	startOuter := floatPoint{
		x: startX + normalX*halfWidth,
		y: startY + normalY*halfWidth,
	}
	endOuter := floatPoint{x: endX + normalX*halfWidth, y: endY + normalY*halfWidth}
	endInner := floatPoint{x: endX - normalX*halfWidth, y: endY - normalY*halfWidth}
	startInner := floatPoint{x: startX - normalX*halfWidth, y: startY - normalY*halfWidth}
	endControl := floatPoint{
		x: endX + directionX*halfWidth*2,
		y: endY + directionY*halfWidth*2,
	}
	startControl := floatPoint{
		x: startX - directionX*halfWidth*2,
		y: startY - directionY*halfWidth*2,
	}
	path := []image.Point{roundFloatPoint(startOuter), roundFloatPoint(endOuter)}
	path = appendQuadratic(path, endOuter, endControl, endInner, maxInt(5, radius))
	path = append(path, roundFloatPoint(startInner))
	path = appendQuadratic(path, startInner, startControl, startOuter, maxInt(5, radius))
	if path[len(path)-1] != path[0] {
		path = append(path, path[0])
	}
	return path
}

type floatPoint struct {
	x float64
	y float64
}

func appendQuadratic(path []image.Point, from, control, to floatPoint, steps int) []image.Point {
	for step := 1; step <= steps; step++ {
		t := float64(step) / float64(steps)
		oneMinusT := 1 - t
		point := roundFloatPoint(floatPoint{
			x: oneMinusT*oneMinusT*from.x + 2*oneMinusT*t*control.x + t*t*to.x,
			y: oneMinusT*oneMinusT*from.y + 2*oneMinusT*t*control.y + t*t*to.y,
		})
		if path[len(path)-1] != point {
			path = append(path, point)
		}
	}
	return path
}

func roundFloatPoint(point floatPoint) image.Point {
	return image.Pt(int(math.Round(point.x)), int(math.Round(point.y)))
}

func drawPolyline(dst *image.RGBA, points []image.Point, width int, stroke color.NRGBA) {
	for index := 1; index < len(points); index++ {
		drawThickLine(dst, points[index-1], points[index], width, stroke)
	}
}

func drawClose(dst *image.RGBA, rectangle image.Rectangle, stroke color.NRGBA) {
	middle := center(rectangle)
	drawThickLine(dst, middle.Add(image.Pt(-4, -4)), middle.Add(image.Pt(4, 4)), 1, stroke)
	drawThickLine(dst, middle.Add(image.Pt(4, -4)), middle.Add(image.Pt(-4, 4)), 1, stroke)
}

func drawChevron(dst *image.RGBA, middle image.Point, right bool, stroke color.NRGBA) {
	direction := -1
	if right {
		direction = 1
	}
	drawThickLine(dst, middle.Add(image.Pt(-direction*3, -6)), middle.Add(image.Pt(direction*3, 0)), 2, stroke)
	drawThickLine(dst, middle.Add(image.Pt(direction*3, 0)), middle.Add(image.Pt(-direction*3, 6)), 2, stroke)
}

func drawReload(dst *image.RGBA, middle image.Point, stroke color.NRGBA) {
	for degree := 35; degree < 330; degree += 9 {
		radians := float64(degree) * math.Pi / 180
		point := middle.Add(image.Pt(int(math.Round(math.Cos(radians)*7)), int(math.Round(math.Sin(radians)*7))))
		fillCircle(dst, point, 1, stroke)
	}
	drawThickLine(dst, middle.Add(image.Pt(5, -6)), middle.Add(image.Pt(8, -6)), 2, stroke)
	drawThickLine(dst, middle.Add(image.Pt(8, -6)), middle.Add(image.Pt(8, -2)), 2, stroke)
}

func drawShield(dst *image.RGBA, middle image.Point, stroke color.NRGBA) {
	points := []image.Point{
		middle.Add(image.Pt(0, -7)), middle.Add(image.Pt(6, -4)), middle.Add(image.Pt(5, 3)),
		middle.Add(image.Pt(0, 7)), middle.Add(image.Pt(-5, 3)), middle.Add(image.Pt(-6, -4)), middle.Add(image.Pt(0, -7)),
	}
	drawPolyline(dst, points, 1, stroke)
}

func drawRailGlyph(dst *image.RGBA, middle image.Point, kind string, stroke color.NRGBA) {
	switch strings.ToLower(kind) {
	case "nodes":
		for _, offset := range []image.Point{image.Pt(-5, -5), image.Pt(5, -5), image.Pt(0, 6)} {
			fillCircle(dst, middle.Add(offset), 2, stroke)
		}
		drawThickLine(dst, middle.Add(image.Pt(-5, -4)), middle.Add(image.Pt(0, 5)), 1, stroke)
		drawThickLine(dst, middle.Add(image.Pt(5, -4)), middle.Add(image.Pt(0, 5)), 1, stroke)
	case "list":
		for y := -5; y <= 5; y += 5 {
			fillCircle(dst, middle.Add(image.Pt(-6, y)), 1, stroke)
			drawThickLine(dst, middle.Add(image.Pt(-2, y)), middle.Add(image.Pt(7, y)), 1, stroke)
		}
	case "pulse":
		points := []image.Point{
			middle.Add(image.Pt(-8, 2)), middle.Add(image.Pt(-4, 2)), middle.Add(image.Pt(-1, -6)),
			middle.Add(image.Pt(2, 7)), middle.Add(image.Pt(5, -2)), middle.Add(image.Pt(8, -2)),
		}
		drawPolyline(dst, points, 1, stroke)
	}
}

func drawGear(dst *image.RGBA, middle image.Point, stroke color.NRGBA) {
	for degree := 0; degree < 360; degree += 45 {
		radians := float64(degree) * math.Pi / 180
		inner := middle.Add(image.Pt(int(math.Round(math.Cos(radians)*5)), int(math.Round(math.Sin(radians)*5))))
		outer := middle.Add(image.Pt(int(math.Round(math.Cos(radians)*8)), int(math.Round(math.Sin(radians)*8))))
		drawThickLine(dst, inner, outer, 2, stroke)
	}
	fillCircle(dst, middle, 5, stroke)
	fillCircle(dst, middle, 2, graphitePalette.top)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
