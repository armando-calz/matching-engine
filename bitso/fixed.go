package bitso

import (
	"errors"
	"fmt"
	"math"
)

var errInvalidDecimal = errors.New("invalid decimal")

// parseFixed converts a non-negative decimal string such as "0.0294324" into an
// integer number of 10^-decimals units, exactly. It never goes through floating
// point, and it rejects values with more decimal places than the unit can hold
// instead of rounding them.
func parseFixed(s string, decimals int) (int64, error) {
	var n int64
	digits, frac := 0, -1 // frac counts digits after the point; -1 before it
	for i := range len(s) {
		c := s[i]
		switch {
		case c == '.' && frac < 0 && digits > 0:
			frac = 0
		case c >= '0' && c <= '9':
			if frac >= 0 {
				if frac == decimals {
					if c != '0' {
						return 0, fmt.Errorf("%w %q: more than %d decimal places", errInvalidDecimal, s, decimals)
					}
					continue // trailing zeros beyond the unit are harmless
				}
				frac++
			}
			if n > (math.MaxInt64-int64(c-'0'))/10 {
				return 0, fmt.Errorf("%w %q: out of range", errInvalidDecimal, s)
			}
			n = n*10 + int64(c-'0')
			digits++
		default:
			return 0, fmt.Errorf("%w %q", errInvalidDecimal, s)
		}
	}
	if digits == 0 || frac == 0 {
		return 0, fmt.Errorf("%w %q", errInvalidDecimal, s)
	}
	for range decimals - max(frac, 0) {
		if n > math.MaxInt64/10 {
			return 0, fmt.Errorf("%w %q: out of range", errInvalidDecimal, s)
		}
		n *= 10
	}
	return n, nil
}
