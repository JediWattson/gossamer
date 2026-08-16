package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

var allInitialComputedValues = map[string]string{
	"background-color":     "rgba(0, 0, 0, 0)",
	"border-bottom-color":  "rgb(0, 0, 0)",
	"border-bottom-style":  "none",
	"border-bottom-width":  "0px",
	"border-left-color":    "rgb(0, 0, 0)",
	"border-left-style":    "none",
	"border-left-width":    "0px",
	"border-right-color":   "rgb(0, 0, 0)",
	"border-right-style":   "none",
	"border-right-width":   "0px",
	"border-top-color":     "rgb(0, 0, 0)",
	"border-top-style":     "none",
	"border-top-width":     "0px",
	"color":                "rgb(0, 0, 0)",
	"display":              "inline",
	"font-size":            "16px",
	"font-weight":          "400",
	"height":               "auto",
	"line-height":          "normal",
	"list-style-type":      "disc",
	"margin-bottom":        "0px",
	"margin-left":          "0px",
	"margin-right":         "0px",
	"margin-top":           "0px",
	"max-width":            "none",
	"min-width":            "0px",
	"opacity":              "1",
	"overflow-x":           "visible",
	"overflow-y":           "visible",
	"padding-bottom":       "0px",
	"padding-left":         "0px",
	"padding-right":        "0px",
	"padding-top":          "0px",
	"text-align":           "start",
	"text-decoration-line": "none",
	"width":                "auto",
}

var allInheritedLonghands = map[string]bool{
	"color":           true,
	"font-size":       true,
	"font-weight":     true,
	"line-height":     true,
	"list-style-type": true,
	"text-align":      true,
}

var allCurrentColorInitialLonghands = map[string]bool{
	"border-bottom-color": true,
	"border-left-color":   true,
	"border-right-color":  true,
	"border-top-color":    true,
}

const allNonInitialDeclarations = `
	display: inline-block;
	color: #123456;
	background-color: #234567;
	font-size: 20px;
	font-weight: 650;
	line-height: 1.5;
	list-style-type: square;
	opacity: 0.4;
	overflow: auto scroll;
	width: 101px;
	height: 102px;
	min-width: 3px;
	max-width: 104px;
	padding: 5px 6px 7px 8px;
	border: 9px solid #345678;
	margin: 10px 11px 12px 13px;
	text-align: end;
	text-decoration-line: underline;
`

func TestAllInitialResetsEverySupportedLonghand(t *testing.T) {
	t.Parallel()

	fixture := computeAllFixture(t, "", allNonInitialDeclarations, "div", allNonInitialDeclarations+"; all: initial")
	assertAllComputedValues(t, fixture.target, allInitialComputedValues)
	for _, property := range style.ComputedPropertyNames(fixture.target) {
		if property == "all" {
			t.Fatal("ComputedPropertyNames() includes the all shorthand")
		}
	}
}

func TestAllInheritCopiesEverySupportedLonghand(t *testing.T) {
	t.Parallel()

	fixture := computeAllFixture(t, "", allNonInitialDeclarations, "span", "all: inherit")
	for property := range allInitialComputedValues {
		got := allComputedValue(t, fixture.target, property)
		want := allComputedValue(t, fixture.parent, property)
		if got != want {
			t.Errorf("inherited %s = %q, want parent value %q", property, got, want)
		}
	}
}

func TestAllInheritOnRootElementUsesInitialValues(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html", dom.Attribute{Name: "style", Value: "all: inherit"})
	html.AppendChild(dom.NewElement("head"))
	html.AppendChild(dom.NewElement("body"))
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{Environment: style.Environment{InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(html)
	if !ok {
		t.Fatal("computed snapshot does not contain the root element")
	}
	assertAllComputedValues(t, computed, allInitialComputedValues)
}

func TestAllUnsetUsesInheritanceOrInitialValuePerLonghand(t *testing.T) {
	t.Parallel()

	fixture := computeAllFixture(t, "", allNonInitialDeclarations, "div", "all: unset")
	for property, initial := range allInitialComputedValues {
		want := initial
		if allInheritedLonghands[property] {
			want = allComputedValue(t, fixture.parent, property)
		} else if allCurrentColorInitialLonghands[property] {
			want = allComputedValue(t, fixture.parent, "color")
		}
		if got := allComputedValue(t, fixture.target, property); got != want {
			t.Errorf("unset %s = %q, want %q", property, got, want)
		}
	}
}

func TestAllFontSizeComputesBeforeDependentLonghands(t *testing.T) {
	t.Parallel()

	fixture := computeAllFixture(
		t,
		"",
		"font-size: 10px",
		"div",
		"font-size: 20px; all: initial; margin-left: 2em",
	)
	assertAllComputedValues(t, fixture.target, map[string]string{
		"font-size":   "16px",
		"margin-left": "32px",
	})
}

func TestAllDoesNotResetCustomProperties(t *testing.T) {
	t.Parallel()

	fixture := computeAllFixture(
		t,
		"",
		"--inherited-tone: #123456",
		"div",
		"--local-tone: #abcdef; all: initial; color: var(--inherited-tone); background-color: var(--local-tone)",
	)
	assertAllComputedValues(t, fixture.target, map[string]string{
		"--inherited-tone": "#123456",
		"--local-tone":     "#abcdef",
		"background-color": "rgb(171, 205, 239)",
		"color":            "rgb(18, 52, 86)",
	})
}

func TestAllRevertRestoresUserAgentValues(t *testing.T) {
	t.Parallel()

	fixture := computeAllFixture(
		t,
		`#target { color: #ff0000; display: block; text-decoration-line: none; width: 55px; all: revert; }`,
		"",
		"a",
		"",
		dom.Attribute{Name: "href", Value: "https://example.test/"},
	)
	assertAllComputedValues(t, fixture.target, map[string]string{
		"color":                "rgb(0, 0, 238)",
		"display":              "inline",
		"text-decoration-line": "underline",
		"width":                "auto",
	})
}

func TestAllRevertLayerRestoresPrecedingLayerValues(t *testing.T) {
	t.Parallel()

	fixture := computeAllFixture(t, `
		@layer base, theme;
		@layer base {
			#target { color: #112233; display: block; width: 41px; padding-left: 7px; }
		}
		@layer theme {
			#target { color: #445566; display: none; width: 99px; padding-left: 12px; all: revert-layer; }
		}
	`, "", "div", "")
	assertAllComputedValues(t, fixture.target, map[string]string{
		"background-color": "rgba(0, 0, 0, 0)",
		"color":            "rgb(17, 34, 51)",
		"display":          "block",
		"padding-left":     "7px",
		"width":            "41px",
	})
}

func TestAllParticipatesInImportanceAndInlineCascadeSteps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		stylesheet   string
		targetInline string
		want         map[string]string
	}{
		{
			name:         "normal inline all beats normal author declarations",
			stylesheet:   `#target { color: #abcdef; display: block; width: 88px; }`,
			targetInline: "all: initial",
			want: map[string]string{
				"color":   "rgb(0, 0, 0)",
				"display": "inline",
				"width":   "auto",
			},
		},
		{
			name:         "important author all beats normal inline declarations",
			stylesheet:   `#target { all: initial !important; }`,
			targetInline: "color: #abcdef; display: block; width: 88px",
			want: map[string]string{
				"color":   "rgb(0, 0, 0)",
				"display": "inline",
				"width":   "auto",
			},
		},
		{
			name: "important inline revert layer reveals author important declarations",
			stylesheet: `
				@layer base {
					#target { color: #112233 !important; display: block !important; width: 21px !important; }
				}
			`,
			targetInline: "color: #abcdef !important; display: none !important; width: 88px !important; all: revert-layer !important",
			want: map[string]string{
				"color":   "rgb(17, 34, 51)",
				"display": "block",
				"width":   "21px",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := computeAllFixture(t, test.stylesheet, "", "div", test.targetInline)
			assertAllComputedValues(t, fixture.target, test.want)
		})
	}
}

func TestAllVarSubstitutionAppliesCSSWideOrInvalidAtComputedValueSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		targetInline string
		wantColor    string
	}{
		{
			name:         "fallback produces valid CSS-wide keyword",
			targetInline: "color: #abcdef; display: block; width: 88px; all: var(--missing, initial)",
			wantColor:    "rgb(0, 0, 0)",
		},
		{
			name:         "missing variable computes as unset without reviving loser",
			targetInline: "color: #abcdef; display: block; width: 88px; all: var(--missing)",
			wantColor:    "rgb(18, 52, 86)",
		},
		{
			name:         "substitution is validated as all rather than as each target",
			targetInline: "--mode: block; color: #abcdef; display: block; width: 88px; all: var(--mode)",
			wantColor:    "rgb(18, 52, 86)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := computeAllFixture(t, "", "color: #123456", "div", test.targetInline)
			assertAllComputedValues(t, fixture.target, map[string]string{
				"color":   test.wantColor,
				"display": "inline",
				"width":   "auto",
			})
		})
	}
}

func TestInvalidAllSpecifiedValueIsDiscardedBeforeCascade(t *testing.T) {
	t.Parallel()

	fixture := computeAllFixture(
		t,
		"",
		"",
		"div",
		"color: #abcdef; display: block; width: 88px; all: red",
	)
	assertAllComputedValues(t, fixture.target, map[string]string{
		"color":   "rgb(171, 205, 239)",
		"display": "block",
		"width":   "88px",
	})
}

type allFixture struct {
	parent style.ComputedStyle
	target style.ComputedStyle
}

func computeAllFixture(
	t *testing.T,
	stylesheet string,
	parentInline string,
	targetTag string,
	targetInline string,
	targetAttributes ...dom.Attribute,
) allFixture {
	t.Helper()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	if stylesheet != "" {
		styleElement := dom.NewElement("style")
		styleElement.AppendChild(dom.NewText(stylesheet))
		head.AppendChild(styleElement)
	}
	parentAttributes := []dom.Attribute(nil)
	if parentInline != "" {
		parentAttributes = append(parentAttributes, dom.Attribute{Name: "style", Value: parentInline})
	}
	parent := dom.NewElement("body", parentAttributes...)
	attributes := []dom.Attribute{{Name: "id", Value: "target"}}
	attributes = append(attributes, targetAttributes...)
	if targetInline != "" {
		attributes = append(attributes, dom.Attribute{Name: "style", Value: targetInline})
	}
	target := dom.NewElement(targetTag, attributes...)
	target.AppendChild(dom.NewText("all shorthand target"))
	parent.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(parent)
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{Environment: style.Environment{
		Width:           640,
		Height:          480,
		MediaType:       "screen",
		InitialFontSize: 16,
	}})
	parentStyle, ok := snapshot.Lookup(parent)
	if !ok {
		t.Fatal("computed snapshot does not contain fixture parent")
	}
	targetStyle, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("computed snapshot does not contain fixture target")
	}
	return allFixture{parent: parentStyle, target: targetStyle}
}

func assertAllComputedValues(t *testing.T, computed style.ComputedStyle, want map[string]string) {
	t.Helper()
	for property, expected := range want {
		if got := allComputedValue(t, computed, property); got != expected {
			t.Errorf("computed %s = %q, want %q", property, got, expected)
		}
	}
}

func allComputedValue(t *testing.T, computed style.ComputedStyle, property string) string {
	t.Helper()
	value, ok := style.ComputedPropertyValue(computed, property)
	if !ok {
		t.Fatalf("ComputedPropertyValue(%q) is unsupported", property)
	}
	return value
}
