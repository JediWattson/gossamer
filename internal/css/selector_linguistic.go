package css

import (
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/bidi"
)

const maxLanguageTagBytes = 4096

// extlangPrefix is generated from the IANA Language Subtag Registry's extlang
// records. Keeping the compact subtag-to-prefix relation is necessary because
// x/text canonicalizes extlangs away without exposing their registered prefix.
var extlangPrefix = map[string]string{
	"aao": "ar", "abh": "ar", "abv": "ar", "acm": "ar", "acq": "ar", "acw": "ar", "acx": "ar", "acy": "ar",
	"adf": "ar", "ads": "sgn", "aeb": "ar", "aec": "ar", "aed": "sgn", "aen": "sgn", "afb": "ar", "afg": "sgn",
	"ajp": "ar", "ajs": "sgn", "apc": "ar", "apd": "ar", "arb": "ar", "arq": "ar", "ars": "ar", "ary": "ar",
	"arz": "ar", "ase": "sgn", "asf": "sgn", "asp": "sgn", "asq": "sgn", "asw": "sgn", "auz": "ar", "avl": "ar",
	"ayh": "ar", "ayl": "ar", "ayn": "ar", "ayp": "ar", "bbz": "ar", "bfi": "sgn", "bfk": "sgn", "bjn": "ms",
	"bog": "sgn", "bqn": "sgn", "bqy": "sgn", "btj": "ms", "bve": "ms", "bvl": "sgn", "bvu": "ms", "bzs": "sgn",
	"cdo": "zh", "cds": "sgn", "cjy": "zh", "cmn": "zh", "cnp": "zh", "coa": "ms", "cpx": "zh", "csc": "sgn",
	"csd": "sgn", "cse": "sgn", "csf": "sgn", "csg": "sgn", "csl": "sgn", "csn": "sgn", "csp": "zh", "csq": "sgn",
	"csr": "sgn", "csx": "sgn", "czh": "zh", "czo": "zh", "doq": "sgn", "dse": "sgn", "dsl": "sgn", "dsz": "sgn",
	"dup": "ms", "dyl": "sgn", "ecs": "sgn", "ehs": "sgn", "esl": "sgn", "esn": "sgn", "eso": "sgn", "eth": "sgn",
	"fcs": "sgn", "fse": "sgn", "fsl": "sgn", "fss": "sgn", "gan": "zh", "gds": "sgn", "gom": "kok", "gse": "sgn",
	"gsg": "sgn", "gsm": "sgn", "gss": "sgn", "gus": "sgn", "hab": "sgn", "haf": "sgn", "hak": "zh", "hds": "sgn",
	"hji": "ms", "hks": "sgn", "hnm": "zh", "hos": "sgn", "hps": "sgn", "hsh": "sgn", "hsl": "sgn", "hsn": "zh",
	"icl": "sgn", "iks": "sgn", "ils": "sgn", "inl": "sgn", "ins": "sgn", "ise": "sgn", "isg": "sgn", "isr": "sgn",
	"jak": "ms", "jax": "ms", "jcs": "sgn", "jhs": "sgn", "jks": "sgn", "jls": "sgn", "jos": "sgn", "jsl": "sgn",
	"jus": "sgn", "kgi": "sgn", "knn": "kok", "kvb": "ms", "kvk": "sgn", "kvr": "ms", "kxd": "ms", "lbs": "sgn",
	"lce": "ms", "lcf": "ms", "lgs": "sgn", "liw": "ms", "lls": "sgn", "lsb": "sgn", "lsc": "sgn", "lsg": "sgn",
	"lsl": "sgn", "lsn": "sgn", "lso": "sgn", "lsp": "sgn", "lst": "sgn", "lsv": "sgn", "lsw": "sgn", "lsy": "sgn",
	"ltg": "lv", "luh": "zh", "lvs": "lv", "lws": "sgn", "lzh": "zh", "max": "ms", "mdl": "sgn", "meo": "ms",
	"mfa": "ms", "mfb": "ms", "mfs": "sgn", "min": "ms", "mnp": "zh", "mqg": "ms", "mre": "sgn", "msd": "sgn",
	"msi": "ms", "msr": "sgn", "mui": "ms", "mzc": "sgn", "mzg": "sgn", "mzy": "sgn", "nan": "zh", "nbs": "sgn",
	"ncs": "sgn", "nsi": "sgn", "nsl": "sgn", "nsp": "sgn", "nsr": "sgn", "nzs": "sgn", "okl": "sgn", "orn": "ms",
	"ors": "ms", "pel": "ms", "pga": "ar", "pgz": "sgn", "pks": "sgn", "prl": "sgn", "prz": "sgn", "psc": "sgn",
	"psd": "sgn", "pse": "ms", "psg": "sgn", "psl": "sgn", "pso": "sgn", "psp": "sgn", "psr": "sgn", "pys": "sgn",
	"rib": "sgn", "rms": "sgn", "rnb": "sgn", "rsi": "sgn", "rsl": "sgn", "rsm": "sgn", "rsn": "sgn", "sdl": "sgn",
	"sfb": "sgn", "sfs": "sgn", "sgg": "sgn", "sgx": "sgn", "shu": "ar", "sjc": "zh", "slf": "sgn", "sls": "sgn",
	"sqk": "sgn", "sqs": "sgn", "sqx": "sgn", "ssh": "ar", "ssp": "sgn", "ssr": "sgn", "svk": "sgn", "swc": "sw",
	"swh": "sw", "swl": "sgn", "syy": "sgn", "szs": "sgn", "tmw": "ms", "tse": "sgn", "tsm": "sgn", "tsq": "sgn",
	"tss": "sgn", "tsy": "sgn", "tza": "sgn", "ugn": "sgn", "ugy": "sgn", "ukl": "sgn", "uks": "sgn", "urk": "ms",
	"uzn": "uz", "uzs": "uz", "vgt": "sgn", "vkk": "ms", "vkt": "ms", "vsi": "sgn", "vsl": "sgn", "vsv": "sgn",
	"wbs": "sgn", "wuu": "zh", "xki": "sgn", "xml": "sgn", "xmm": "ms", "xms": "sgn", "yds": "sgn", "ygs": "sgn",
	"yhs": "sgn", "ysl": "sgn", "ysm": "sgn", "yue": "zh", "zhk": "sgn", "zib": "sgn", "zlm": "ms", "zmi": "ms",
	"zsl": "sgn", "zsm": "ms",
}

func matchesLanguageRanges(node *dom.Node, ranges []string, context MatchContext, state *selectorMatchState) bool {
	languageTag, ok := elementContentLanguage(node, context, state)
	if !ok || len(languageTag) > maxLanguageTagBytes {
		return false
	}
	languageTag = canonicalLanguageTag(languageTag)
	for _, languageRange := range ranges {
		if !state.take() || len(languageRange) > maxLanguageTagBytes {
			return false
		}
		if extendedLanguageRangeMatches(canonicalLanguageRange(languageRange), languageTag) {
			return true
		}
	}
	return false
}

// elementContentLanguage implements the DOM-backed part of HTML's language
// algorithm. The protocol-level fallback is explicit MatchContext state so the
// selector package never reaches outside its coherent DOM read.
func elementContentLanguage(node *dom.Node, context MatchContext, state *selectorMatchState) (string, bool) {
	for current := node; current != nil && current.Type == dom.ElementNode; {
		if !state.take() {
			return "", false
		}
		// Attribute namespace identity is not yet retained by dom.Attribute.
		// The HTML parser serializes a foreign xml:lang attribute with this
		// qualified name; on HTML elements the same literal attribute has no
		// language-processing effect.
		if current.NamespaceURI != dom.HTMLNamespace {
			if value, found := qualifiedAttributeValue(current, "xml:lang"); found {
				return value, true
			}
		}
		if current.NamespaceURI == dom.HTMLNamespace || current.NamespaceURI == dom.SVGNamespace {
			if value, found := attributeValue(current, "lang"); found {
				return value, true
			}
		}
		if current.Parent == nil || current.Parent.Type != dom.ElementNode {
			break
		}
		current = current.Parent
	}
	return context.DefaultLanguage, true
}

func qualifiedAttributeValue(node *dom.Node, name string) (string, bool) {
	for _, attribute := range node.Attributes {
		if attribute.Name == name {
			return attribute.Value, true
		}
	}
	return "", false
}

func canonicalLanguageTag(value string) string {
	return canonicalExtlangForm(value)
}

func canonicalLanguageRange(value string) string {
	value = lowerASCII(value)
	parts := strings.Split(value, "-")
	for index, part := range parts {
		if part != "*" {
			continue
		}
		if index == 0 {
			return value
		}
		prefix := canonicalExtlangForm(strings.Join(parts[:index], "-"))
		return prefix + "-" + strings.Join(parts[index:], "-")
	}
	return canonicalExtlangForm(value)
}

// canonicalExtlangForm applies the BCP 47 canonicalization available in
// x/text, then restores a registered extlang prefix when the parser confirms
// that prefix+language is a valid equivalent tag. Unknown tags intentionally
// pass through so :lang(xyzzy) can match lang="xyzzy".
func canonicalExtlangForm(value string) string {
	if value == "" || len(value) > maxLanguageTagBytes {
		return lowerASCII(value)
	}
	lowered := lowerASCII(value)
	parts := strings.Split(lowered, "-")
	if len(parts) == 1 {
		if prefix := extlangPrefix[parts[0]]; prefix != "" {
			lowered = prefix + "-" + parts[0]
			value = lowered
			parts = strings.Split(lowered, "-")
		}
	}
	if len(parts) >= 2 && isASCIIAlphaSubtag(parts[0], 2, 3) && isASCIIAlphaSubtag(parts[1], 3, 3) {
		prefix, registered := extlangPrefix[parts[1]]
		if !registered || prefix != parts[0] || len(parts) >= 3 && isASCIIAlphaSubtag(parts[2], 3, 3) {
			return lowered
		}
	}
	tag, err := language.BCP47.Parse(value)
	if err != nil {
		return lowered
	}
	canonical := lowerASCII(tag.String())
	base, _, _ := tag.Raw()
	baseName := lowerASCII(base.String())
	if baseName == "" || baseName == "und" {
		return canonical
	}
	if prefix := extlangPrefix[baseName]; prefix != "" {
		if canonical == baseName || strings.HasPrefix(canonical, baseName+"-") {
			return prefix + "-" + canonical
		}
	}
	return canonical
}

func isASCIIAlphaSubtag(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		if character < 'a' || character > 'z' {
			return false
		}
	}
	return true
}

// extendedLanguageRangeMatches is RFC 4647 section 3.3.2 extended filtering,
// with Selectors 4's explicit behavior for tagged and untagged content.
func extendedLanguageRangeMatches(languageRange, languageTag string) bool {
	if languageRange == "" {
		return languageTag == ""
	}
	if languageTag == "" {
		return false
	}
	if languageRange == "*" {
		return true
	}
	rangeParts := strings.Split(lowerASCII(languageRange), "-")
	tagParts := strings.Split(lowerASCII(languageTag), "-")
	if len(rangeParts) == 0 || len(tagParts) == 0 || rangeParts[0] != "*" && rangeParts[0] != tagParts[0] {
		return false
	}
	rangeIndex, tagIndex := 1, 1
	for rangeIndex < len(rangeParts) {
		if rangeParts[rangeIndex] == "*" {
			rangeIndex++
			continue
		}
		if tagIndex >= len(tagParts) {
			return false
		}
		if rangeParts[rangeIndex] == tagParts[tagIndex] {
			rangeIndex++
			tagIndex++
			continue
		}
		if len(tagParts[tagIndex]) == 1 {
			return false
		}
		tagIndex++
	}
	return true
}

type selectorDirection uint8

const (
	directionUnknown selectorDirection = iota
	directionLeftToRight
	directionRightToLeft
)

func matchesDirectionality(node *dom.Node, requested string, state *selectorMatchState) bool {
	if requested != "ltr" && requested != "rtl" {
		return false
	}
	direction, ok := elementDirectionality(node, state)
	if !ok {
		return false
	}
	if requested == "rtl" {
		return direction == directionRightToLeft
	}
	return direction == directionLeftToRight
}

// elementDirectionality implements HTML's directionality algorithm for the
// light DOM represented by dom.Node. Shadow slots are deliberately absent from
// the current DOM model, while HTML and foreign-namespace parent inheritance,
// bdi, telephone controls, explicit states, and auto directionality are kept.
func elementDirectionality(node *dom.Node, state *selectorMatchState) (selectorDirection, bool) {
	for current := node; current != nil && current.Type == dom.ElementNode; {
		if !state.take() {
			return directionUnknown, false
		}
		if current.NamespaceURI == dom.HTMLNamespace {
			switch htmlDirAttributeState(current) {
			case "ltr":
				return directionLeftToRight, true
			case "rtl":
				return directionRightToLeft, true
			case "auto":
				direction, found, ok := autoDirectionality(current, state)
				if !ok {
					return directionUnknown, false
				}
				if found {
					return direction, true
				}
				return directionLeftToRight, true
			}
			if isHTMLElementNamed(current, "bdi") {
				direction, found, ok := autoDirectionality(current, state)
				if !ok {
					return directionUnknown, false
				}
				if found {
					return direction, true
				}
				return directionLeftToRight, true
			}
			if isHTMLElementNamed(current, "input") && normalizedInputType(current) == "tel" {
				return directionLeftToRight, true
			}
		}
		if current.Parent == nil || current.Parent.Type != dom.ElementNode {
			return directionLeftToRight, true
		}
		current = current.Parent
	}
	return directionLeftToRight, true
}

func htmlDirAttributeState(node *dom.Node) string {
	if node == nil || node.NamespaceURI != dom.HTMLNamespace {
		return ""
	}
	value, found := attributeValue(node, "dir")
	if !found {
		return ""
	}
	switch lowerASCII(value) {
	case "ltr", "rtl", "auto":
		return lowerASCII(value)
	default:
		return ""
	}
}

func autoDirectionality(node *dom.Node, state *selectorMatchState) (selectorDirection, bool, bool) {
	if isAutoDirectionalityFormControl(node) {
		return formControlAutoDirectionality(node, state)
	}
	return containedTextAutoDirectionality(node, state)
}

func isAutoDirectionalityFormControl(node *dom.Node) bool {
	if isHTMLElementNamed(node, "textarea") {
		return true
	}
	if !isHTMLElementNamed(node, "input") {
		return false
	}
	switch normalizedInputType(node) {
	case "hidden", "text", "search", "tel", "url", "email", "password", "submit", "reset", "button":
		return true
	default:
		return false
	}
}

func normalizedInputType(node *dom.Node) string {
	typeName := lowerASCII(attributeOrDefault(node, "type", "text"))
	switch typeName {
	case "hidden", "text", "search", "tel", "url", "email", "password", "date", "month", "week", "time",
		"datetime-local", "number", "range", "color", "checkbox", "radio", "file", "submit", "image", "reset", "button":
		return typeName
	default:
		return "text"
	}
}

func formControlAutoDirectionality(node *dom.Node, state *selectorMatchState) (selectorDirection, bool, bool) {
	if node.Control != nil && node.Control.ValueDirty {
		return directionFromFormValue(node.Control.Value, state)
	}
	if isHTMLElementNamed(node, "input") {
		value, _ := attributeValue(node, "value")
		return directionFromFormValue(value, state)
	}
	return descendantTextDirectionality(node, state, true)
}

func directionFromFormValue(value string, state *selectorMatchState) (selectorDirection, bool, bool) {
	direction, found, ok := firstStrongDirection(value, state)
	if !ok || found {
		return direction, found, ok
	}
	if value != "" {
		return directionLeftToRight, true, true
	}
	return directionUnknown, false, true
}

func containedTextAutoDirectionality(root *dom.Node, state *selectorMatchState) (selectorDirection, bool, bool) {
	stack := make([]*dom.Node, 0, len(root.Children))
	for index := len(root.Children) - 1; index >= 0; index-- {
		stack = append(stack, root.Children[index])
	}
	for len(stack) != 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if !state.take() {
			return directionUnknown, false, false
		}
		if current == nil {
			continue
		}
		if current.Type == dom.TextNode {
			direction, found, ok := firstStrongDirection(current.Data, state)
			if !ok || found {
				return direction, found, ok
			}
			continue
		}
		if current.Type != dom.ElementNode || excludesAutoDirectionText(current) {
			continue
		}
		for index := len(current.Children) - 1; index >= 0; index-- {
			stack = append(stack, current.Children[index])
		}
	}
	return directionUnknown, false, true
}

func descendantTextDirectionality(root *dom.Node, state *selectorMatchState, nonEmptyIsLTR bool) (selectorDirection, bool, bool) {
	stack := make([]*dom.Node, 0, len(root.Children))
	for index := len(root.Children) - 1; index >= 0; index-- {
		stack = append(stack, root.Children[index])
	}
	nonEmpty := false
	for len(stack) != 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if !state.take() {
			return directionUnknown, false, false
		}
		if current == nil {
			continue
		}
		if current.Type == dom.TextNode {
			nonEmpty = nonEmpty || current.Data != ""
			direction, found, ok := firstStrongDirection(current.Data, state)
			if !ok || found {
				return direction, found, ok
			}
			continue
		}
		for index := len(current.Children) - 1; index >= 0; index-- {
			stack = append(stack, current.Children[index])
		}
	}
	if nonEmpty && nonEmptyIsLTR {
		return directionLeftToRight, true, true
	}
	return directionUnknown, false, true
}

func excludesAutoDirectionText(node *dom.Node) bool {
	if node == nil || node.NamespaceURI != dom.HTMLNamespace {
		return false
	}
	if htmlDirAttributeState(node) != "" {
		return true
	}
	switch lowerASCII(node.Data) {
	case "bdi", "script", "style", "textarea":
		return true
	default:
		return false
	}
}

func firstStrongDirection(value string, state *selectorMatchState) (selectorDirection, bool, bool) {
	for _, character := range value {
		if !state.take() {
			return directionUnknown, false, false
		}
		properties, _ := bidi.LookupRune(character)
		switch properties.Class() {
		case bidi.L:
			return directionLeftToRight, true, true
		case bidi.R, bidi.AL:
			return directionRightToLeft, true, true
		}
	}
	return directionUnknown, false, true
}
