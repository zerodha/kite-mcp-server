package instruments

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

var ErrOptionChainNotFound = errors.New("option chain not found")

// OptionStrike retains an unmatched side as null rather than inventing a contract.
type OptionStrike struct {
	Strike float64     `json:"strike"`
	Call   *Instrument `json:"call"`
	Put    *Instrument `json:"put"`
}

// OptionChain is contract reference data from one complete catalog snapshot.
// It contains no live quotes, streaming state, or computed Greeks.
type OptionChain struct {
	Exchange           string         `json:"exchange"`
	UnderlyingExchange string         `json:"underlying_exchange"`
	UnderlyingName     string         `json:"underlying_name"`
	Underlying         *Instrument    `json:"underlying"`
	Future             *Instrument    `json:"future"`
	Expiry             string         `json:"expiry"`
	ExpiryType         string         `json:"expiry_type"`
	Expiries           []string       `json:"expiries"`
	Strikes            []OptionStrike `json:"strikes"`
	LoadedAt           time.Time      `json:"loaded_at"`
	ActivePolicy       string         `json:"active_policy"`
}

type optionChainIndex map[string]map[string]OptionChain

// The aliases follow Kite ticker's option-chain mapping. Compact keys also
// accept user inputs such as NIFTY50 without changing instrument identifiers.
var optionUnderlyingAliases = map[string]string{
	"NIFTY": "NIFTY 50", "NIFTY50": "NIFTY 50",
	"BANKNIFTY": "NIFTY BANK", "NIFTYBANK": "NIFTY BANK",
	"MIDCPNIFTY": "NIFTY MID SELECT", "NIFTYMIDSELECT": "NIFTY MID SELECT",
	"NIFTYMIDCAPSELECT(MIDCPNIFTY)": "NIFTY MID SELECT",
	"FINNIFTY":                      "NIFTY FIN SERVICE", "NIFTYFINSERVICE": "NIFTY FIN SERVICE",
	"NIFTYNXT50": "NIFTY NEXT 50", "NIFTYNEXT50": "NIFTY NEXT 50",
	"FOCIT": "BSE FOCUSED IT", "BSEFOCUSEDIT": "BSE FOCUSED IT",
	"SENSEX50": "SNSX50", "SNSX50": "SNSX50",
	"BSEINDEXBANKEX": "BANKEX", "BANKEX": "BANKEX",
}

func optionName(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	key := strings.ReplaceAll(name, " ", "")
	if alias, ok := optionUnderlyingAliases[key]; ok {
		return alias
	}
	return name
}
func derivativeExchange(exchange string) string {
	switch exchange = strings.ToUpper(strings.TrimSpace(exchange)); exchange {
	case "NSE":
		return "NFO"
	case "BSE":
		return "BFO"
	default:
		return exchange
	}
}
func spotExchange(exchange string) string {
	switch exchange {
	case "NFO":
		return "NSE"
	case "BFO":
		return "BSE"
	default:
		return exchange
	}
}
func validOptionDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}
func copyOptionInstrument(inst *Instrument) *Instrument {
	if inst == nil {
		return nil
	}
	out := *inst
	return &out
}
func buildOptionChainIndex(catalog map[uint32]*Instrument) optionChainIndex {
	// Stable ordering makes duplicate expiry/strike/side resolution deterministic.
	instruments := make([]*Instrument, 0, len(catalog))
	for _, inst := range catalog {
		if inst != nil {
			instruments = append(instruments, inst)
		}
	}
	sort.Slice(instruments, func(i, j int) bool {
		if instruments[i].ID != instruments[j].ID {
			return instruments[i].ID < instruments[j].ID
		}
		return instruments[i].InstrumentToken < instruments[j].InstrumentToken
	})
	index := make(optionChainIndex)
	strikesByChain := make(map[string]map[string]map[float64]OptionStrike)
	for _, inst := range instruments {
		if !inst.Active || !validOptionDate(inst.ExpiryDate) || strings.TrimSpace(inst.Name) == "" {
			continue
		}
		if inst.InstrumentType != "CE" && inst.InstrumentType != "PE" && inst.InstrumentType != "FUT" {
			continue
		}
		if inst.InstrumentType != "FUT" && (inst.Strike <= 0 || math.IsNaN(inst.Strike) || math.IsInf(inst.Strike, 0)) {
			continue
		}
		exchange := derivativeExchange(inst.Exchange)
		// Cash exchange instruments must never be treated as derivative contracts.
		if exchange != strings.ToUpper(inst.Exchange) {
			continue
		}
		name := optionName(inst.Name)
		key := exchange + ":" + name
		if index[key] == nil {
			index[key] = make(map[string]OptionChain)
			strikesByChain[key] = make(map[string]map[float64]OptionStrike)
		}
		chain, exists := index[key][inst.ExpiryDate]
		if !exists {
			strikesByChain[key][inst.ExpiryDate] = make(map[float64]OptionStrike)
			chain = OptionChain{Exchange: exchange, UnderlyingExchange: spotExchange(exchange), UnderlyingName: name, Expiry: inst.ExpiryDate, Strikes: []OptionStrike{}, ActivePolicy: "active_derivatives_only"}
		}
		if inst.InstrumentType == "FUT" {
			if chain.Future == nil {
				chain.Future = copyOptionInstrument(inst)
			}
		} else {
			if chain.ExpiryType == "" {
				chain.ExpiryType = inst.ExpiryType
			}
			strike := strikesByChain[key][inst.ExpiryDate][inst.Strike]
			strike.Strike = inst.Strike
			if inst.InstrumentType == "CE" && strike.Call == nil {
				strike.Call = copyOptionInstrument(inst)
			}
			if inst.InstrumentType == "PE" && strike.Put == nil {
				strike.Put = copyOptionInstrument(inst)
			}
			strikesByChain[key][inst.ExpiryDate][inst.Strike] = strike
		}
		index[key][inst.ExpiryDate] = chain
	}
	// Index real cash/index identities once; avoid rescanning the full catalog
	// for every expiry. Trading symbols take priority over display names.
	spotBySymbol := make(map[string]*Instrument)
	spotByName := make(map[string]*Instrument)
	for _, inst := range instruments {
		if inst.InstrumentType != "EQ" && inst.Segment != segIndices {
			continue
		}
		if key := inst.Exchange + ":" + optionName(inst.Tradingsymbol); spotBySymbol[key] == nil {
			spotBySymbol[key] = inst
		}
		if key := inst.Exchange + ":" + optionName(inst.Name); spotByName[key] == nil {
			spotByName[key] = inst
		}
	}
	// A derivative future is exposed separately and is never silently
	// relabeled as the underlying spot price.
	for key, expiries := range index {
		for expiry, chain := range expiries {
			for _, strike := range strikesByChain[key][expiry] {
				chain.Strikes = append(chain.Strikes, strike)
			}
			sort.Slice(chain.Strikes, func(i, j int) bool { return chain.Strikes[i].Strike < chain.Strikes[j].Strike })
			if len(chain.Strikes) == 0 {
				delete(expiries, expiry)
				continue
			}
			spotKey := chain.UnderlyingExchange + ":" + chain.UnderlyingName
			spot := spotBySymbol[spotKey]
			if spot == nil {
				spot = spotByName[spotKey]
			}
			chain.Underlying = copyOptionInstrument(spot)
			expiries[expiry] = chain
		}
		if len(expiries) == 0 {
			delete(index, key)
		}
	}
	return index
}

// GetOptionChain resolves a full selected-expiry chain from the manager's
// complete indexed snapshot. Empty expiry selects the nearest unexpired IST
// date, including today. Explicit expired dates are rejected. Returned values
// are independent copies, safe for pagination or caller mutation.
func (m *Manager) GetOptionChain(exchange, underlying, expiry string) (OptionChain, error) {
	exchange = derivativeExchange(exchange)
	underlying = optionName(underlying)
	if exchange == "" || underlying == "" {
		return OptionChain{}, fmt.Errorf("exchange and underlying are required")
	}
	if expiry != "" && !validOptionDate(expiry) {
		return OptionChain{}, fmt.Errorf("expiry must be YYYY-MM-DD")
	}
	today := time.Now().In(istLocation).Format("2006-01-02")
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	expiries := m.optionChains[exchange+":"+underlying]
	available := make([]string, 0, len(expiries))
	for date := range expiries {
		if date >= today {
			available = append(available, date)
		}
	}
	sort.Strings(available)
	if len(available) == 0 {
		return OptionChain{}, fmt.Errorf("%w: %s:%s has no active, unexpired option contracts", ErrOptionChainNotFound, exchange, underlying)
	}
	if expiry == "" {
		expiry = available[0]
	}
	source, ok := expiries[expiry]
	if !ok || expiry < today {
		return OptionChain{}, fmt.Errorf("%w: %s:%s expiry %s", ErrOptionChainNotFound, exchange, underlying, expiry)
	}
	out := source
	out.Expiries = available
	out.LoadedAt = m.optionChainsLoadedAt
	out.Underlying = copyOptionInstrument(source.Underlying)
	out.Future = copyOptionInstrument(source.Future)
	out.Strikes = make([]OptionStrike, len(source.Strikes))
	for i, strike := range source.Strikes {
		out.Strikes[i] = OptionStrike{Strike: strike.Strike, Call: copyOptionInstrument(strike.Call), Put: copyOptionInstrument(strike.Put)}
	}
	return out, nil
}
