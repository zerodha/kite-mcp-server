package instruments

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func chainDate(days int) string {
	return time.Now().In(istLocation).AddDate(0, 0, days).Format("2006-01-02")
}
func chainInstrument(token uint32, exchange, name, typ, date string, strike float64) *Instrument {
	symbol := fmt.Sprintf("%s%s%.0f%s", name, date, strike, typ)
	return &Instrument{ID: exchange + ":" + symbol, InstrumentToken: token, Exchange: exchange, Tradingsymbol: symbol, Name: name, InstrumentType: typ, ExpiryDate: date, Strike: strike, Active: true, ExpiryType: "weekly", Segment: exchange + "-OPT"}
}
func TestOptionChainSnapshotPairingAndCopies(t *testing.T) {
	m := newTestManagerWithoutUpdate()
	near, far := chainDate(0), chainDate(20)
	call := chainInstrument(10, "NFO", "NIFTY", "CE", near, 25000)
	put := chainInstrument(11, "NFO", "NIFTY", "PE", near, 25000)
	missing := chainInstrument(12, "NFO", "NIFTY", "PE", near, 24000)
	inactive := chainInstrument(13, "NFO", "NIFTY", "CE", near, 23000)
	inactive.Active = false
	future := chainInstrument(14, "NFO", "NIFTY", "FUT", near, 0)
	spot := &Instrument{ID: "NSE:NIFTY 50", Exchange: "NSE", InstrumentToken: 15, Tradingsymbol: "NIFTY 50", Name: "NIFTY 50", Segment: "INDICES"}
	m.LoadMap(map[uint32]*Instrument{10: call, 11: put, 12: missing, 13: inactive, 14: future, 15: spot, 16: chainInstrument(16, "NFO", "NIFTY", "CE", far, 25000), 17: chainInstrument(17, "NFO", "NIFTY", "CE", chainDate(-1), 25000)})
	chain, err := m.GetOptionChain("NSE", "NIFTY50", "")
	if err != nil {
		t.Fatal(err)
	}
	if chain.Expiry != near || len(chain.Expiries) != 2 || chain.Expiries[1] != far || len(chain.Strikes) != 2 {
		t.Fatalf("unexpected chain: %+v", chain)
	}
	if chain.Strikes[0].Strike != 24000 || chain.Strikes[0].Call != nil || chain.Strikes[0].Put == nil || chain.Strikes[1].Call == nil || chain.Strikes[1].Put == nil {
		t.Fatalf("missing/paired sides incorrect: %+v", chain.Strikes)
	}
	if chain.Underlying == nil || chain.Underlying.InstrumentToken != 15 || chain.Future == nil || chain.Future.InstrumentToken != 14 || chain.LoadedAt.IsZero() || chain.ActivePolicy != "active_derivatives_only" {
		t.Fatalf("metadata incorrect: %+v", chain)
	}
	chain.Underlying.Name = "changed"
	chain.Future.Name = "changed"
	chain.Strikes[1].Call.Name = "changed"
	chain.Strikes[0].Strike = 1
	chain.Expiries[0] = "changed"
	next, err := m.GetOptionChain("NFO", "NIFTY 50", near)
	if err != nil {
		t.Fatal(err)
	}
	if next.Underlying.Name != "NIFTY 50" || next.Future.Name != "NIFTY" || next.Strikes[1].Call.Name != "NIFTY" || next.Strikes[0].Strike != 24000 || next.Expiries[0] != near {
		t.Fatal("caller mutation affected indexed snapshot")
	}
	if _, err := m.GetOptionChain("NFO", "NIFTY", chainDate(-1)); !errors.Is(err, ErrOptionChainNotFound) {
		t.Fatalf("expired date accepted: %v", err)
	}
	if _, err := m.GetOptionChain("NFO", "NIFTY", "bad-date"); err == nil {
		t.Fatal("invalid expiry accepted")
	}
}
func TestOptionChainAliasesAndNoInventedUnderlying(t *testing.T) {
	for _, names := range []struct {
		exchange, spot string
		aliases        []string
	}{
		{"NFO", "NIFTY 50", []string{"NIFTY", "NIFTY50", "NIFTY 50"}},
		{"NFO", "NIFTY BANK", []string{"BANKNIFTY", "NIFTY BANK"}},
		{"NFO", "NIFTY MID SELECT", []string{"MIDCPNIFTY", "NIFTY MID SELECT", "NIFTY MIDCAP SELECT (MIDCPNIFTY)"}},
		{"NFO", "NIFTY FIN SERVICE", []string{"FINNIFTY", "NIFTY FIN SERVICE"}},
		{"NFO", "NIFTY NEXT 50", []string{"NIFTYNXT50", "NIFTY NEXT 50"}},
		{"BFO", "BSE FOCUSED IT", []string{"FOCIT", "BSE FOCUSED IT"}},
		{"BFO", "SNSX50", []string{"SENSEX50", "SNSX50"}},
		{"BFO", "BANKEX", []string{"BSE INDEX BANKEX", "BANKEX"}},
	} {
		t.Run(names.spot, func(t *testing.T) {
			m := newTestManagerWithoutUpdate()
			date := chainDate(10)
			option := chainInstrument(1, names.exchange, names.aliases[0], "CE", date, 100)
			spot := &Instrument{ID: spotExchange(names.exchange) + ":" + names.spot, Exchange: spotExchange(names.exchange), Tradingsymbol: names.spot, Name: names.spot, InstrumentToken: 2, Segment: segIndices}
			m.LoadMap(map[uint32]*Instrument{1: option, 2: spot})
			for _, alias := range names.aliases {
				for _, exchange := range []string{names.exchange, spot.Exchange} {
					chain, err := m.GetOptionChain(exchange, alias, date)
					if err != nil || chain.Underlying == nil || chain.Underlying.InstrumentToken != 2 {
						t.Fatalf("alias %s:%s failed: %+v %v", exchange, alias, chain, err)
					}
				}
			}
		})
	}
	m := newTestManagerWithoutUpdate()
	date := chainDate(10)
	m.LoadMap(map[uint32]*Instrument{1: chainInstrument(1, "CDS", "USDINR", "CE", date, 100), 2: chainInstrument(2, "CDS", "USDINR", "FUT", date, 0)})
	chain, err := m.GetOptionChain("CDS", "USDINR", date)
	if err != nil {
		t.Fatal(err)
	}
	if chain.Underlying != nil || chain.Future == nil {
		t.Fatal("future must remain separate from missing cash underlying")
	}
}
func TestOptionChainFullCatalogInsertAndDeterministicDuplicate(t *testing.T) {
	m := newTestManagerWithoutUpdate()
	date := chainDate(10)
	catalog := make(map[uint32]*Instrument)
	for token := uint32(1); token <= 2201; token++ {
		catalog[token] = chainInstrument(token, "NFO", "BIG", "CE", date, float64(token))
	}
	// Duplicate side selection must not depend on map iteration order.
	duplicate := chainInstrument(9999, "NFO", "BIG", "CE", date, 1)
	duplicate.ID = catalog[1].ID
	catalog[9999] = duplicate
	m.LoadMap(catalog)
	chain, err := m.GetOptionChain("NFO", "BIG", date)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain.Strikes) != 2201 || chain.Strikes[0].Call.InstrumentToken != 1 {
		t.Fatalf("catalog truncated or unstable duplicate: %d", len(chain.Strikes))
	}
	m.Insert(chainInstrument(10000, "NFO", "BIG", "PE", date, 1))
	chain, err = m.GetOptionChain("NFO", "BIG", date)
	if err != nil || chain.Strikes[0].Put == nil {
		t.Fatalf("Insert did not rebuild chain: %v", err)
	}
	m.LoadMap(map[uint32]*Instrument{10001: chainInstrument(10001, "NFO", "BIG", "PE", date, 2202)})
	chain, _ = m.GetOptionChain("NFO", "BIG", date)
	if len(chain.Strikes) != 2202 {
		t.Fatal("LoadMap must preserve its existing additive semantics")
	}
}
func TestOptionChainDailyUpdateAtomicallyReplacesIndex(t *testing.T) {
	m := newTestManagerWithoutUpdate()
	date := chainDate(10)
	m.LoadMap(map[uint32]*Instrument{1: chainInstrument(1, "NFO", "OLD", "CE", date, 100)})
	replacement := chainInstrument(2, "BFO", "NEW", "PE", date, 200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(replacement) }))
	defer server.Close()
	original := instrumentsURL
	instrumentsURL = server.URL
	defer func() { instrumentsURL = original }()
	if err := m.ForceUpdateInstruments(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetOptionChain("NFO", "OLD", date); !errors.Is(err, ErrOptionChainNotFound) {
		t.Fatalf("old chain retained after replacement: %v", err)
	}
	if chain, err := m.GetOptionChain("BFO", "NEW", date); err != nil || len(chain.Strikes) != 1 || chain.Strikes[0].Put.InstrumentToken != 2 {
		t.Fatalf("replacement chain missing: %+v %v", chain, err)
	}
}
func TestOptionChainConcurrentInsertAndRead(t *testing.T) {
	m := newTestManagerWithoutUpdate()
	date := chainDate(10)
	m.Insert(chainInstrument(1, "NFO", "TEST", "CE", date, 100))
	var group sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for i := 0; i < 30; i++ {
				if worker == 0 {
					m.Insert(chainInstrument(uint32(i+2), "NFO", "TEST", "PE", date, float64(i+101)))
				} else {
					chain, err := m.GetOptionChain("NFO", "TEST", date)
					if err != nil || len(chain.Strikes) == 0 {
						t.Errorf("incomplete snapshot: %v", err)
						return
					}
					chain.Strikes[0].Call.Name = "caller-owned"
				}
			}
		}(worker)
	}
	group.Wait()
}
