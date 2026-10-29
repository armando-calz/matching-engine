package orderbook

import (
	"errors"
	"testing"
)

func TestNewLimitOrderValidates(t *testing.T) {
	tests := []struct {
		name     string
		side     Side
		price    Price
		quantity Quantity
		wantErr  error
	}{
		{"valid buy", Buy, 100, 5, nil},
		{"valid sell", Sell, 1, 1, nil},
		{"zero side", Side(0), 100, 5, ErrInvalidSide},
		{"unknown side", Side(9), 100, 5, ErrInvalidSide},
		{"zero price", Buy, 0, 5, ErrInvalidPrice},
		{"negative price", Sell, -1, 5, ErrInvalidPrice},
		{"zero quantity", Buy, 100, 0, ErrInvalidQuantity},
		{"negative quantity", Sell, 100, -3, ErrInvalidQuantity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order, err := NewLimitOrder(7, tt.side, tt.price, tt.quantity)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (order.ID != 7 || order.Side != tt.side || order.Price != tt.price || order.Quantity != tt.quantity) {
				t.Fatalf("order = %+v", order)
			}
		})
	}
}

func TestCrosses(t *testing.T) {
	buy := Order{Side: Buy, Price: 100}
	sell := Order{Side: Sell, Price: 100}

	tests := []struct {
		name  string
		order Order
		price Price
		want  bool
	}{
		{"buy crosses a cheaper ask", buy, 99, true},
		{"buy crosses an ask at its price", buy, 100, true},
		{"buy does not cross a dearer ask", buy, 101, false},
		{"sell crosses a higher bid", sell, 101, true},
		{"sell crosses a bid at its price", sell, 100, true},
		{"sell does not cross a lower bid", sell, 99, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.order.Crosses(tt.price); got != tt.want {
				t.Fatalf("Crosses(%d) = %v, want %v", tt.price, got, tt.want)
			}
		})
	}
}

func TestSide(t *testing.T) {
	if Buy.Opposite() != Sell || Sell.Opposite() != Buy {
		t.Fatal("Opposite")
	}
	if Buy.String() != "buy" || Sell.String() != "sell" || Side(9).String() != "Side(9)" {
		t.Fatal("String")
	}
}
