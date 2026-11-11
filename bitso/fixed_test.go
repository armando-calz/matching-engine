package bitso

import (
	"errors"
	"testing"
)

func TestParseFixed(t *testing.T) {
	tests := []struct {
		in       string
		decimals int
		want     int64
		wantErr  bool
	}{
		{"1368510", 2, 136851000, false},
		{"0.0294324", 8, 2943240, false},
		{"0.00000001", 8, 1, false},
		{"1.5", 2, 150, false},
		{"1.50000", 2, 150, false}, // zeros beyond the unit are fine
		{"0", 8, 0, false},
		{"92233720368547758.07", 2, 9223372036854775807, false},
		{"92233720368547758.08", 2, 0, true}, // one unit over int64
		{"0.000000001", 8, 0, true},          // would need rounding
		{"", 2, 0, true},
		{".5", 2, 0, true},
		{"5.", 2, 0, true},
		{"1.2.3", 2, 0, true},
		{"-1", 2, 0, true},
		{"1e5", 2, 0, true},
		{" 1", 2, 0, true},
	}
	for _, tt := range tests {
		got, err := parseFixed(tt.in, tt.decimals)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("parseFixed(%q, %d) = %d, %v; want %d, error %t", tt.in, tt.decimals, got, err, tt.want, tt.wantErr)
		}
		if err != nil && !errors.Is(err, errInvalidDecimal) {
			t.Errorf("parseFixed(%q): error %v is not errInvalidDecimal", tt.in, err)
		}
	}
}
