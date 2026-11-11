package bitso

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/armando-calz/matching-engine/orderbook"
)

// Market describes one Bitso book and the integer units used for it locally.
type Market struct {
	Book           string // e.g. "btc_mxn"
	PriceDecimals  int    // a price unit is 10^-PriceDecimals of the quote currency
	AmountDecimals int    // an amount unit is 10^-AmountDecimals of the base currency
}

// BTCMXN uses cents of a peso for prices and satoshis for amounts.
var BTCMXN = Market{Book: "btc_mxn", PriceDecimals: 2, AmountDecimals: 8}

// messageType returns the "type" of a WebSocket message: "diff-orders", "ka"
// (keep-alive), or the type of a subscription response.
func messageType(data []byte) (string, error) {
	var m struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("websocket message: %w", err)
	}
	return m.Type, nil
}

// decodeDiff decodes a diff-orders message.
func (m Market) decodeDiff(data []byte) (Diff, error) {
	var msg struct {
		Book     string `json:"book"`
		Sequence uint64 `json:"sequence"`
		Payload  []struct {
			ID     string `json:"o"`
			Side   *int   `json:"t"`
			Rate   string `json:"r"`
			Amount string `json:"a"`
			Status string `json:"s"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return Diff{}, fmt.Errorf("diff-orders: %w", err)
	}
	if msg.Book != m.Book {
		return Diff{}, fmt.Errorf("diff-orders: book %q, want %q", msg.Book, m.Book)
	}
	d := Diff{Seq: msg.Sequence, Updates: make([]Update, 0, len(msg.Payload))}
	for _, p := range msg.Payload {
		u := Update{ID: p.ID}
		var err error
		switch {
		case p.ID == "":
			err = fmt.Errorf("missing order id")
		case p.Side == nil || (*p.Side != 0 && *p.Side != 1):
			err = fmt.Errorf("order %s: invalid side", p.ID)
		}
		if err == nil {
			u.Side = orderbook.Buy
			if *p.Side == 1 {
				u.Side = orderbook.Sell
			}
			switch p.Status {
			case "open":
				u.Status = Open
				u.Price, u.Amount, err = m.parse(p.Rate, p.Amount)
			case "cancelled":
				u.Status = Cancelled
			case "completed":
				u.Status = Completed
			default:
				err = fmt.Errorf("order %s: unknown status %q", p.ID, p.Status)
			}
		}
		if err != nil {
			return Diff{}, fmt.Errorf("diff-orders %d: %w", msg.Sequence, err)
		}
		d.Updates = append(d.Updates, u)
	}
	return d, nil
}

// decodeSnapshot decodes the response of GET /v3/order_book/?book=...&aggregate=false.
func (m Market) decodeSnapshot(data []byte) (Snapshot, error) {
	type order struct {
		Book   string `json:"book"`
		Price  string `json:"price"`
		Amount string `json:"amount"`
		ID     string `json:"oid"`
	}
	var resp struct {
		Success bool `json:"success"`
		Payload struct {
			Sequence string  `json:"sequence"`
			Bids     []order `json:"bids"`
			Asks     []order `json:"asks"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return Snapshot{}, fmt.Errorf("order book snapshot: %w", err)
	}
	if !resp.Success {
		return Snapshot{}, fmt.Errorf("order book snapshot: success is false")
	}
	seq, err := strconv.ParseUint(resp.Payload.Sequence, 10, 64)
	if err != nil {
		return Snapshot{}, fmt.Errorf("order book snapshot: sequence: %w", err)
	}
	s := Snapshot{Seq: seq, Orders: make([]Update, 0, len(resp.Payload.Bids)+len(resp.Payload.Asks))}
	for _, side := range []struct {
		side   orderbook.Side
		orders []order
	}{{orderbook.Buy, resp.Payload.Bids}, {orderbook.Sell, resp.Payload.Asks}} {
		for _, o := range side.orders {
			if o.Book != m.Book || o.ID == "" {
				return Snapshot{}, fmt.Errorf("order book snapshot: unexpected order %+v", o)
			}
			price, amount, err := m.parse(o.Price, o.Amount)
			if err != nil {
				return Snapshot{}, fmt.Errorf("order book snapshot: order %s: %w", o.ID, err)
			}
			s.Orders = append(s.Orders, Update{ID: o.ID, Side: side.side, Price: price, Amount: amount, Status: Open})
		}
	}
	return s, nil
}

func (m Market) parse(price, amount string) (orderbook.Price, orderbook.Quantity, error) {
	p, err := parseFixed(price, m.PriceDecimals)
	if err != nil {
		return 0, 0, fmt.Errorf("price: %w", err)
	}
	a, err := parseFixed(amount, m.AmountDecimals)
	if err != nil {
		return 0, 0, fmt.Errorf("amount: %w", err)
	}
	return orderbook.Price(p), orderbook.Quantity(a), nil
}
