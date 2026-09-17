package aec

import (
	"fmt"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
)

// matchTimeout bounds every match. regexp2 has no linear-time guarantee.
const matchTimeout = time.Second

// CompilePattern compiles a PHP-style delimited pattern such as
// `/try\s*\{/i` or `#^\s*rm #m`. The delimiter is the first character and
// must be `/` or `#`; trailing flags map to regexp2 options: i, m, s.
func CompilePattern(phpPattern string) (*regexp2.Regexp, error) {
	if phpPattern == "" {
		return nil, fmt.Errorf("pattern is empty")
	}
	delim := phpPattern[0]
	if delim != '/' && delim != '#' {
		return nil, fmt.Errorf("pattern %q must start with / or #", phpPattern)
	}
	end := strings.LastIndexByte(phpPattern[1:], delim)
	if end < 0 {
		return nil, fmt.Errorf("pattern %q has no closing %c", phpPattern, delim)
	}
	end++ // offset into phpPattern
	body := phpPattern[1:end]
	flags := phpPattern[end+1:]

	opts := regexp2.None
	for _, f := range flags {
		switch f {
		case 'i':
			opts |= regexp2.IgnoreCase
		case 'm':
			opts |= regexp2.Multiline
		case 's':
			opts |= regexp2.Singleline
		default:
			return nil, fmt.Errorf("pattern %q has unsupported flag %q", phpPattern, f)
		}
	}

	re, err := regexp2.Compile(body, opts)
	if err != nil {
		return nil, fmt.Errorf("pattern %q: %w", phpPattern, err)
	}
	re.MatchTimeout = matchTimeout
	return re, nil
}
