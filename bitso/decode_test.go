package bitso

import (
	"slices"
	"strings"
	"testing"

	"github.com/armando-calz/matching-engine/orderbook"
)

// Message shapes follow Bitso's public documentation; ids and values are made up.

func TestDecodeDiff(t *testing.T) {
	msg := `{"type":"diff-orders","book":"btc_mxn","sequence":2734,"sent":1675555546102,"payload":[
		{"o":"ord-open","d":1675555546102,"r":"1368510","t":0,"a":"0.25","v":"342127.5","s":"open"},
		{"o":"ord-cancelled","d":1677784699999,"r":"1369600.5","t":1,"s":"cancelled","z":1912784699999},
		{"o":"ord-done","d":1677784699999,"r":"1369600","t":1,"s":"completed"}]}`

	if typ, err := messageType([]byte(msg)); err != nil || typ != "diff-orders" {
		t.Fatalf("messageType = %q, %v", typ, err)
	}
	d, err := BTCMXN.decodeDiff([]byte(msg))
	if err != nil {
		t.Fatal(err)
	}
	want := Diff{Seq: 2734, Updates: []Update{
		{ID: "ord-open", Side: orderbook.Buy, Price: 136851000, Amount: 25000000, Status: Open},
		{ID: "ord-cancelled", Side: orderbook.Sell, Status: Cancelled},
		{ID: "ord-done", Side: orderbook.Sell, Status: Completed},
	}}
	if d.Seq != want.Seq || !slices.Equal(d.Updates, want.Updates) {
		t.Fatalf("diff = %+v\nwant %+v", d, want)
	}
}

func TestDecodeDiffRejectsMalformedMessages(t *testing.T) {
	for _, tt := range []struct{ name, payload, wantErr string }{
		{"other book", `"book":"eth_mxn","payload":[]`, "book"},
		{"no side", `"book":"btc_mxn","payload":[{"o":"x","r":"1","a":"1","s":"open"}]`, "side"},
		{"bad side", `"book":"btc_mxn","payload":[{"o":"x","t":2,"r":"1","a":"1","s":"open"}]`, "side"},
		{"no id", `"book":"btc_mxn","payload":[{"t":0,"r":"1","a":"1","s":"open"}]`, "id"},
		{"unknown status", `"book":"btc_mxn","payload":[{"o":"x","t":0,"s":"partial"}]`, "status"},
		{"amount too precise", `"book":"btc_mxn","payload":[{"o":"x","t":0,"r":"1","a":"0.000000001","s":"open"}]`, "amount"},
		{"float price", `"book":"btc_mxn","payload":[{"o":"x","t":0,"r":"1e6","a":"1","s":"open"}]`, "price"},
	} {
		_, err := BTCMXN.decodeDiff([]byte(`{"type":"diff-orders","sequence":1,` + tt.payload + `}`))
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: err = %v, want one mentioning %q", tt.name, err, tt.wantErr)
		}
	}
}

func TestDecodeSnapshot(t *testing.T) {
	body := `{"success":true,"payload":{"updated_at":"2026-11-11T00:18:28+00:00","sequence":"4595901082",
		"bids":[{"book":"btc_mxn","price":"1368510","amount":"0.0294324","oid":"bid-1"}],
		"asks":[{"book":"btc_mxn","price":"1369600","amount":"0.06789","oid":"ask-1"},
		        {"book":"btc_mxn","price":"1369610.5","amount":"1","oid":"ask-2"}]}}`
	s, err := BTCMXN.decodeSnapshot([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	want := []Update{
		{ID: "bid-1", Side: orderbook.Buy, Price: 136851000, Amount: 2943240, Status: Open},
		{ID: "ask-1", Side: orderbook.Sell, Price: 136960000, Amount: 6789000, Status: Open},
		{ID: "ask-2", Side: orderbook.Sell, Price: 136961050, Amount: 100000000, Status: Open},
	}
	if s.Seq != 4595901082 || !slices.Equal(s.Orders, want) {
		t.Fatalf("snapshot = %+v", s)
	}

	for _, bad := range []string{
		`{"success":false}`,
		`{"success":true,"payload":{"sequence":"x","bids":[],"asks":[]}}`,
		`{"success":true,"payload":{"sequence":"1","bids":[{"book":"eth_mxn","price":"1","amount":"1","oid":"a"}],"asks":[]}}`,
		`not json`,
	} {
		if _, err := BTCMXN.decodeSnapshot([]byte(bad)); err == nil {
			t.Errorf("decodeSnapshot(%s) accepted it", bad)
		}
	}
}

func TestKeepAliveType(t *testing.T) {
	if typ, err := messageType([]byte(`{"type":"ka"}`)); err != nil || typ != "ka" {
		t.Fatalf("messageType = %q, %v", typ, err)
	}
}
