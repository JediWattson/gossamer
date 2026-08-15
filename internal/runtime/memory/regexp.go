package memory

import (
	"fmt"
	"strings"
)

type RegExpFlags uint16

const (
	RegExpHasIndices RegExpFlags = 1 << iota
	RegExpGlobal
	RegExpIgnoreCase
	RegExpMultiline
	RegExpDotAll
	RegExpUnicode
	RegExpUnicodeSets
	RegExpSticky
)

const allRegExpFlags = RegExpHasIndices | RegExpGlobal | RegExpIgnoreCase | RegExpMultiline | RegExpDotAll | RegExpUnicode | RegExpUnicodeSets | RegExpSticky

func ParseRegExpFlags(text string) (RegExpFlags, error) {
	var flags RegExpFlags
	for _, character := range text {
		var flag RegExpFlags
		switch character {
		case 'd':
			flag = RegExpHasIndices
		case 'g':
			flag = RegExpGlobal
		case 'i':
			flag = RegExpIgnoreCase
		case 'm':
			flag = RegExpMultiline
		case 's':
			flag = RegExpDotAll
		case 'u':
			flag = RegExpUnicode
		case 'v':
			flag = RegExpUnicodeSets
		case 'y':
			flag = RegExpSticky
		default:
			return 0, fmt.Errorf("%w: unknown flag %q", ErrInvalidRegExp, character)
		}
		if flags&flag != 0 {
			return 0, fmt.Errorf("%w: duplicate flag %q", ErrInvalidRegExp, character)
		}
		flags |= flag
	}
	if flags&RegExpUnicode != 0 && flags&RegExpUnicodeSets != 0 {
		return 0, fmt.Errorf("%w: flags u and v are mutually exclusive", ErrInvalidRegExp)
	}
	return flags, nil
}

func (flags RegExpFlags) String() string {
	var result strings.Builder
	for _, item := range []struct {
		character byte
		flag      RegExpFlags
	}{
		{'d', RegExpHasIndices},
		{'g', RegExpGlobal},
		{'i', RegExpIgnoreCase},
		{'m', RegExpMultiline},
		{'s', RegExpDotAll},
		{'u', RegExpUnicode},
		{'v', RegExpUnicodeSets},
		{'y', RegExpSticky},
	} {
		if flags&item.flag != 0 {
			result.WriteByte(item.character)
		}
	}
	return result.String()
}

type RegExp struct {
	Pattern   Ref
	Flags     RegExpFlags
	LastIndex uint64
}

func cloneRegExp(expression RegExp) RegExp { return expression }
