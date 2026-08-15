package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestCascadeOriginOrderForNormalDeclarations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  cascadeOriginFixtureOptions
		property string
		want     string
	}{
		{
			name: "user agent supplies the lowest normal value",
			fixture: cascadeOriginFixtureOptions{
				tag:        "a",
				attributes: []dom.Attribute{{Name: "href", Value: "https://example.test/"}},
			},
			property: "color",
			want:     "rgb(0, 0, 238)",
		},
		{
			name: "user beats user agent",
			fixture: cascadeOriginFixtureOptions{
				tag:        "a",
				attributes: []dom.Attribute{{Name: "href", Value: "https://example.test/"}},
				userCSS:    []string{`#target { color: #112233; }`},
			},
			property: "color",
			want:     "rgb(17, 34, 51)",
		},
		{
			name: "presentational hint beats user",
			fixture: cascadeOriginFixtureOptions{
				tag:        "img",
				attributes: []dom.Attribute{{Name: "width", Value: "41"}},
				userCSS:    []string{`#target { width: 22px; }`},
			},
			property: "width",
			want:     "41px",
		},
		{
			name: "author stylesheet beats presentational hint",
			fixture: cascadeOriginFixtureOptions{
				tag:        "img",
				attributes: []dom.Attribute{{Name: "width", Value: "41"}},
				userCSS:    []string{`#target { width: 22px; }`},
				authorCSS:  `#target { width: 63px; }`,
			},
			property: "width",
			want:     "63px",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			computed := computeCascadeOriginFixture(t, test.fixture)
			assertCascadeOriginValue(t, computed, test.property, test.want)
		})
	}
}

func TestCascadeOriginOrderForImportantDeclarations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  cascadeOriginFixtureOptions
		property string
		want     string
	}{
		{
			name: "author important beats presentational hint and normal inline style",
			fixture: cascadeOriginFixtureOptions{
				tag:          "img",
				attributes:   []dom.Attribute{{Name: "width", Value: "41"}},
				authorCSS:    `#target { width: 63px !important; }`,
				targetInline: "width: 74px",
			},
			property: "width",
			want:     "63px",
		},
		{
			name: "important inline style beats author stylesheet",
			fixture: cascadeOriginFixtureOptions{
				authorCSS:    `#target { color: #112233 !important; }`,
				targetInline: "color: #445566 !important",
			},
			property: "color",
			want:     "rgb(68, 85, 102)",
		},
		{
			name: "user important beats important inline style",
			fixture: cascadeOriginFixtureOptions{
				userCSS:      []string{`#target { color: #112233 !important; }`},
				authorCSS:    `#target { color: #445566 !important; }`,
				targetInline: "color: #778899 !important",
			},
			property: "color",
			want:     "rgb(17, 34, 51)",
		},
		{
			name: "user agent important beats user and author important",
			fixture: cascadeOriginFixtureOptions{
				userAgentCSS: []string{`#target { color: #010203 !important; }`},
				userCSS:      []string{`#target { color: #112233 !important; }`},
				authorCSS:    `#target { color: #445566 !important; }`,
				targetInline: "color: #778899 !important",
			},
			property: "color",
			want:     "rgb(1, 2, 3)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			computed := computeCascadeOriginFixture(t, test.fixture)
			assertCascadeOriginValue(t, computed, test.property, test.want)
		})
	}
}

func TestInlineStylesAreTheirOwnAuthorCascadeStep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		authorCSS    string
		targetInline string
		want         string
	}{
		{
			name:         "normal inline beats normal stylesheet",
			authorCSS:    `#target { color: #112233; }`,
			targetInline: "color: #445566",
			want:         "rgb(68, 85, 102)",
		},
		{
			name:         "normal inline loses to important stylesheet",
			authorCSS:    `#target { color: #112233 !important; }`,
			targetInline: "color: #445566",
			want:         "rgb(17, 34, 51)",
		},
		{
			name:         "important inline beats important stylesheet",
			authorCSS:    `#target { color: #112233 !important; }`,
			targetInline: "color: #445566 !important",
			want:         "rgb(68, 85, 102)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
				authorCSS:    test.authorCSS,
				targetInline: test.targetInline,
			})
			assertCascadeOriginValue(t, computed, "color", test.want)
		})
	}
}

func TestCascadeLayersAreRankedWithinEachOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture cascadeOriginFixtureOptions
		want    string
	}{
		{
			name: "later user agent layer wins for normal declarations",
			fixture: cascadeOriginFixtureOptions{userAgentCSS: []string{`
				@layer base, theme;
				@layer base { #target { color: #112233; } }
				@layer theme { #target { color: #445566; } }
			`}},
			want: "rgb(68, 85, 102)",
		},
		{
			name: "earlier user agent layer wins for important declarations",
			fixture: cascadeOriginFixtureOptions{userAgentCSS: []string{`
				@layer base, theme;
				@layer base { #target { color: #112233 !important; } }
				@layer theme { #target { color: #445566 !important; } }
			`}},
			want: "rgb(17, 34, 51)",
		},
		{
			name: "later user layer wins for normal declarations",
			fixture: cascadeOriginFixtureOptions{userCSS: []string{`
				@layer base, theme;
				@layer base { #target { color: #112233; } }
				@layer theme { #target { color: #445566; } }
			`}},
			want: "rgb(68, 85, 102)",
		},
		{
			name: "earlier user layer wins for important declarations",
			fixture: cascadeOriginFixtureOptions{userCSS: []string{`
				@layer base, theme;
				@layer base { #target { color: #112233 !important; } }
				@layer theme { #target { color: #445566 !important; } }
			`}},
			want: "rgb(17, 34, 51)",
		},
		{
			name: "later author layer wins for normal declarations",
			fixture: cascadeOriginFixtureOptions{authorCSS: `
				@layer base, theme;
				@layer base { #target { color: #112233; } }
				@layer theme { #target { color: #445566; } }
			`},
			want: "rgb(68, 85, 102)",
		},
		{
			name: "earlier author layer wins for important declarations",
			fixture: cascadeOriginFixtureOptions{authorCSS: `
				@layer base, theme;
				@layer base { #target { color: #112233 !important; } }
				@layer theme { #target { color: #445566 !important; } }
			`},
			want: "rgb(17, 34, 51)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			computed := computeCascadeOriginFixture(t, test.fixture)
			assertCascadeOriginValue(t, computed, "color", test.want)
		})
	}
}

func TestRevertRollsBackAuthorThenUserToUserAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture cascadeOriginFixtureOptions
		want    string
	}{
		{
			name: "author revert reveals user origin",
			fixture: cascadeOriginFixtureOptions{
				tag:        "a",
				attributes: []dom.Attribute{{Name: "href", Value: "https://example.test/"}},
				userCSS:    []string{`#target { color: #112233; }`},
				authorCSS:  `#target { color: revert; }`,
			},
			want: "rgb(17, 34, 51)",
		},
		{
			name: "user revert reveals user agent origin",
			fixture: cascadeOriginFixtureOptions{
				tag:        "a",
				attributes: []dom.Attribute{{Name: "href", Value: "https://example.test/"}},
				userCSS:    []string{`#target { color: revert; }`},
				authorCSS:  `#target { color: revert; }`,
			},
			want: "rgb(0, 0, 238)",
		},
		{
			name: "important user revert suppresses the author origin",
			fixture: cascadeOriginFixtureOptions{
				tag:          "a",
				attributes:   []dom.Attribute{{Name: "href", Value: "https://example.test/"}},
				userCSS:      []string{`#target { color: revert !important; }`},
				authorCSS:    `#target { color: #445566 !important; }`,
				targetInline: "color: #778899 !important",
			},
			want: "rgb(0, 0, 238)",
		},
		{
			name: "important user agent revert resolves to the initial value",
			fixture: cascadeOriginFixtureOptions{
				userAgentCSS: []string{`#target { color: revert !important; }`},
				userCSS:      []string{`#target { color: #112233 !important; }`},
				authorCSS:    `#target { color: #445566 !important; }`,
				targetInline: "color: #778899 !important",
			},
			want: "rgb(0, 0, 0)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			computed := computeCascadeOriginFixture(t, test.fixture)
			assertCascadeOriginValue(t, computed, "color", test.want)
		})
	}
}

func TestRevertLayerPreservesPresentationalHintWhileRevertRemovesIt(t *testing.T) {
	t.Parallel()

	userCSS := []string{`#target { width: 19px; }`}
	tests := []struct {
		name      string
		authorCSS string
		want      string
	}{
		{
			name: "revert layer reveals the hint",
			authorCSS: `
				@layer theme {
					#target { width: 72px; width: revert-layer; }
				}
			`,
			want: "41px",
		},
		{
			name:      "revert removes the hint with the author origin",
			authorCSS: `#target { width: 72px; width: revert; }`,
			want:      "19px",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
				tag:        "img",
				attributes: []dom.Attribute{{Name: "width", Value: "41"}},
				userCSS:    userCSS,
				authorCSS:  test.authorCSS,
			})
			assertCascadeOriginValue(t, computed, "width", test.want)
		})
	}
}

func TestOriginAwareRollbackDoesNotScanForbiddenCascadeLevels(t *testing.T) {
	t.Parallel()

	t.Run("author important revert discards all author levels and the hint", func(t *testing.T) {
		t.Parallel()
		computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
			tag:        "img",
			attributes: []dom.Attribute{{Name: "width", Value: "41"}},
			userCSS:    []string{`#target { width: 19px; }`},
			authorCSS: `
				#target {
					width: 63px;
					width: 72px !important;
					width: revert !important;
				}
			`,
		})
		assertCascadeOriginValue(t, computed, "width", "19px")
	})

	t.Run("exhausted user important layer rolls back to user agent", func(t *testing.T) {
		t.Parallel()
		computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
			tag:        "a",
			attributes: []dom.Attribute{{Name: "href", Value: "https://example.test/"}},
			userCSS: []string{`
				@layer only {
					#target { color: revert-layer !important; }
				}
			`},
			authorCSS: `
				#target {
					color: #112233;
					color: #445566 !important;
				}
			`,
			targetInline: "color: #778899 !important",
		})
		assertCascadeOriginValue(t, computed, "color", "rgb(0, 0, 238)")
	})

	t.Run("first author important layer rolls back past all author values to hint", func(t *testing.T) {
		t.Parallel()
		computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
			tag:        "img",
			attributes: []dom.Attribute{{Name: "width", Value: "41"}},
			authorCSS: `
				@layer base, theme;
				@layer base {
					#target { width: 11px; width: revert-layer !important; }
				}
				@layer theme {
					#target { width: 22px; width: 33px !important; }
				}
				#target { width: 44px; width: 55px !important; }
			`,
		})
		assertCascadeOriginValue(t, computed, "width", "41px")
	})

	t.Run("later author important layer reveals an earlier normal layer", func(t *testing.T) {
		t.Parallel()
		computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
			authorCSS: `
				@layer base, theme;
				@layer base { #target { width: 17px; } }
				@layer theme { #target { width: 23px; width: revert-layer !important; } }
				#target { width: 31px; width: 37px !important; }
			`,
		})
		assertCascadeOriginValue(t, computed, "width", "17px")
	})

	t.Run("important inline revert layer reveals author important and skips inline normal", func(t *testing.T) {
		t.Parallel()
		computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
			authorCSS:    `#target { color: #112233 !important; }`,
			targetInline: "color: #445566; color: revert-layer !important",
		})
		assertCascadeOriginValue(t, computed, "color", "rgb(17, 34, 51)")
	})

	t.Run("author layer rollback can continue through author revert to user", func(t *testing.T) {
		t.Parallel()
		computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
			userCSS: []string{`#target { --tone: #112233; }`},
			authorCSS: `
				@layer base, high;
				@layer base { #target { --tone: revert; } }
				@layer high { #target { --tone: #445566; --tone: revert-layer; } }
				#target { color: var(--tone); }
			`,
		})
		assertCascadeOriginValue(t, computed, "--tone", "#112233")
		assertCascadeOriginValue(t, computed, "color", "rgb(17, 34, 51)")
	})
}

func TestAllRevertRollsEveryLonghandOutOfTheAuthorOrigin(t *testing.T) {
	t.Parallel()

	computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
		tag:        "img",
		attributes: []dom.Attribute{{Name: "width", Value: "91"}},
		userCSS: []string{`
			#target {
				color: #112233;
				display: block;
				width: 34px;
			}
		`},
		authorCSS: `
			#target {
				color: #445566;
				display: none;
				width: 82px;
				all: revert;
			}
		`,
	})

	assertCascadeOriginValues(t, computed, map[string]string{
		"color":   "rgb(17, 34, 51)",
		"display": "block",
		"width":   "34px",
	})
}

func TestAllRevertLayerRollsEachLonghandAcrossTheHintBoundary(t *testing.T) {
	t.Parallel()

	computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
		tag:        "img",
		attributes: []dom.Attribute{{Name: "width", Value: "91"}},
		userCSS: []string{`
			#target {
				color: #112233;
				display: block;
				width: 34px;
				--tone: #123456;
			}
		`},
		authorCSS: `
			@layer theme {
				#target {
					color: #445566;
					display: none;
					width: 82px;
					--tone: #abcdef;
					all: revert-layer;
				}
			}
		`,
	})

	assertCascadeOriginValues(t, computed, map[string]string{
		"--tone":  "#abcdef",
		"color":   "rgb(17, 34, 51)",
		"display": "block",
		"width":   "91px",
	})
}

func TestCustomPropertiesRollBackAcrossLayersAndOrigins(t *testing.T) {
	t.Parallel()

	t.Run("author revert layer reveals an earlier author layer", func(t *testing.T) {
		t.Parallel()
		computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
			userCSS: []string{`#target { --tone: #112233; }`},
			authorCSS: `
				@layer base, theme;
				@layer base { #target { --tone: #445566; } }
				@layer theme { #target { --tone: revert-layer; } }
				#target { color: var(--tone); }
			`,
		})
		assertCascadeOriginValue(t, computed, "--tone", "#445566")
		assertCascadeOriginValue(t, computed, "color", "rgb(68, 85, 102)")
	})

	t.Run("author revert reveals a user custom property", func(t *testing.T) {
		t.Parallel()
		computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
			userCSS:   []string{`#target { --space: 21px; }`},
			authorCSS: `#target { --space: revert; width: var(--space); }`,
		})
		assertCascadeOriginValue(t, computed, "--space", "21px")
		assertCascadeOriginValue(t, computed, "width", "21px")
	})

	t.Run("user revert reveals the inherited custom property", func(t *testing.T) {
		t.Parallel()
		computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
			parentInline: "--tone: #123456",
			userCSS:      []string{`#target { --tone: revert; }`},
			authorCSS:    `#target { --tone: revert; color: var(--tone); }`,
		})
		assertCascadeOriginValue(t, computed, "--tone", "#123456")
		assertCascadeOriginValue(t, computed, "color", "rgb(18, 52, 86)")
	})
}

func TestInvalidAtComputedValueTimeUsesUnsetWithoutRevivingAnotherOrigin(t *testing.T) {
	t.Parallel()

	computed := computeCascadeOriginFixture(t, cascadeOriginFixtureOptions{
		parentInline: "color: #123456",
		userCSS: []string{`
			#target {
				color: #112233;
				width: 45px;
			}
		`},
		authorCSS: `
			#target {
				color: var(--missing);
				width: var(--missing);
			}
		`,
	})

	assertCascadeOriginValues(t, computed, map[string]string{
		"color": "rgb(18, 52, 86)",
		"width": "auto",
	})
}

type cascadeOriginFixtureOptions struct {
	tag          string
	attributes   []dom.Attribute
	parentInline string
	targetInline string
	userAgentCSS []string
	userCSS      []string
	authorCSS    string
}

func computeCascadeOriginFixture(t *testing.T, options cascadeOriginFixtureOptions) style.ComputedStyle {
	t.Helper()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	if options.authorCSS != "" {
		styleElement := dom.NewElement("style")
		styleElement.AppendChild(dom.NewText(options.authorCSS))
		head.AppendChild(styleElement)
	}

	parentAttributes := []dom.Attribute(nil)
	if options.parentInline != "" {
		parentAttributes = append(parentAttributes, dom.Attribute{Name: "style", Value: options.parentInline})
	}
	parent := dom.NewElement("body", parentAttributes...)
	tag := options.tag
	if tag == "" {
		tag = "div"
	}
	targetAttributes := []dom.Attribute{{Name: "id", Value: "target"}}
	targetAttributes = append(targetAttributes, options.attributes...)
	if options.targetInline != "" {
		targetAttributes = append(targetAttributes, dom.Attribute{Name: "style", Value: options.targetInline})
	}
	target := dom.NewElement(tag, targetAttributes...)
	target.AppendChild(dom.NewText("cascade origin target"))
	parent.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(parent)
	document.AppendChild(html)

	userStylesheets := make([]css.Stylesheet, 0, len(options.userCSS))
	for _, source := range options.userCSS {
		stylesheet, err := css.Parse(source)
		if err != nil {
			t.Fatalf("css.Parse(user stylesheet) error = %v", err)
		}
		userStylesheets = append(userStylesheets, stylesheet)
	}
	userAgentStylesheets := make([]css.Stylesheet, 0, len(options.userAgentCSS))
	for _, source := range options.userAgentCSS {
		stylesheet, err := css.Parse(source)
		if err != nil {
			t.Fatalf("css.Parse(user-agent stylesheet) error = %v", err)
		}
		userAgentStylesheets = append(userAgentStylesheets, stylesheet)
	}
	snapshot := style.Compute(document, style.Input{
		Environment: style.Environment{
			Width:           640,
			Height:          480,
			MediaType:       "screen",
			InitialFontSize: 16,
		},
		UserStylesheets:      userStylesheets,
		UserAgentStylesheets: userAgentStylesheets,
	})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("computed snapshot does not contain cascade origin target")
	}
	return computed
}

func assertCascadeOriginValues(t *testing.T, computed style.ComputedStyle, want map[string]string) {
	t.Helper()
	for property, expected := range want {
		assertCascadeOriginValue(t, computed, property, expected)
	}
}

func assertCascadeOriginValue(t *testing.T, computed style.ComputedStyle, property, want string) {
	t.Helper()
	got, ok := style.ComputedPropertyValue(computed, property)
	if !ok {
		t.Fatalf("ComputedPropertyValue(%q) is unsupported", property)
	}
	if got != want {
		t.Errorf("computed %s = %q, want %q", property, got, want)
	}
}
