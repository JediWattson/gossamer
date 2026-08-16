package css

const maxSelectorNesting = 128

// parseSelectorList parses an ordinary, unforgiving selector list: one invalid
// member invalidates the entire list.
func parseSelectorList(source string) ([]Selector, bool) {
	return parseTokenSelectorListAtDepth(source, 0)
}

func parseSelectorListAtDepth(source string, nesting int) ([]Selector, bool) {
	return parseTokenSelectorListAtDepth(source, nesting)
}

func isHexDigit(character byte) bool {
	return character >= '0' && character <= '9' ||
		character >= 'a' && character <= 'f' ||
		character >= 'A' && character <= 'F'
}

func hexDigitValue(character byte) int {
	switch {
	case character >= '0' && character <= '9':
		return int(character - '0')
	case character >= 'a' && character <= 'f':
		return int(character-'a') + 10
	default:
		return int(character-'A') + 10
	}
}
