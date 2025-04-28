package instruments

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// TODO: maybe this needs to be under a mutex

const (
	instrumentsURL = "https://api.kite.trade/instruments.json"
	segIndices     = "INDICES"
)

var (
	// ErrInstrumentNotFound is returned when instrument was not found in the
	// loaded map.
	ErrInstrumentNotFound = errors.New("instrument not found")

	// ErrSegmentNotFound is returned when segment was not found in the
	// loaded map.
	ErrSegmentNotFound = errors.New("instrument segment not found")
)

type Manager struct {
	isinToInstruments map[string][]*Instrument
	idToInst          map[string]*Instrument
	idToToken         map[string]uint32
	tokenToInstrument map[uint32]*Instrument

	// NSE=1, BSE=2 etc. This is extracted from instrument tokens
	// as they're loaded.
	segmentIDs map[string]uint32

	lastUpdated time.Time
}

func NewManager() *Manager {
	m := &Manager{
		isinToInstruments: make(map[string][]*Instrument),
		idToInst:          make(map[string]*Instrument),
		idToToken:         make(map[string]uint32),
		tokenToInstrument: make(map[uint32]*Instrument),
		segmentIDs:        make(map[string]uint32),

		lastUpdated: time.Now(),
	}

	if err := m.UpdateInstruments(); err != nil {
		log.Fatal(err)
	}

	return m
}

func isPreviousDayIST(t time.Time) bool {
	// Define IST location (UTC+5:30)
	ist, _ := time.LoadLocation("Asia/Kolkata")

	// Convert current time to IST
	nowIST := time.Now().In(ist)

	// Convert the provided time to IST
	tIST := t.In(ist)

	// Extract date components (year, month, day) from both times
	nowYear, nowMonth, nowDay := nowIST.Date()
	tYear, tMonth, tDay := tIST.Date()

	// Compare date components to check if t is from the previous day or earlier
	if tYear < nowYear {
		return true
	}
	if tYear == nowYear && tMonth < nowMonth {
		return true
	}
	if tYear == nowYear && tMonth == nowMonth && tDay < nowDay {
		return true
	}

	return false
}

func (m *Manager) UpdateInstruments() error {
	// Only update if we have no instruments loaded or if the last update was from a previous day
	if m.Count() > 0 && !isPreviousDayIST(m.lastUpdated) {
		// No need to update if we have instruments and they were loaded today
		return nil
	}

	log.Println("Updating instruments...")

	// HTTP GET request to instruments URL
	req, err := http.NewRequest("GET", instrumentsURL, nil)
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	// Add compression header
	req.Header.Add("Accept-Encoding", "gzip")

	// Create HTTP client
	client := &http.Client{}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error fetching instruments: %v", err)
	}
	defer resp.Body.Close()

	// Check if response is compressed
	var reader io.Reader
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("error creating gzip reader: %v", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	} else {
		reader = resp.Body
	}

	mp := map[uint32]*Instrument{}

	// Process JSONL formatted data
	scanner := bufio.NewScanner(reader)

	// Read and process each line as a separate JSON object
	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines
		if len(strings.TrimSpace(line)) == 0 {
			continue
		}

		var instrument Instrument
		if err := json.Unmarshal([]byte(line), &instrument); err != nil {
			return fmt.Errorf("error parsing instrument JSON: %v (line: %s)", err, line)
		}

		// Process each instrument
		mp[instrument.InstrumentToken] = &instrument
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading instruments file: %v", err)
	}

	// Update the last updated timestamp
	m.lastUpdated = time.Now()

	m.LoadMap(mp)

	log.Println("Loaded instruments", m.Count())

	return nil
}

// LoadMap from tokenToInstrument map to the manager.
func (m *Manager) LoadMap(tokenToInstrument map[uint32]*Instrument) {
	for _, inst := range tokenToInstrument {
		m.Insert(inst)
	}
}

// Insert inserts a new instrument.
func (m *Manager) Insert(inst *Instrument) {
	// ISIN -> Instrument
	if inst.ISIN != "" {
		if _, ok := m.isinToInstruments[inst.ISIN]; !ok {
			m.isinToInstruments[inst.ISIN] = []*Instrument{}
		}
		m.isinToInstruments[inst.ISIN] = append(m.isinToInstruments[inst.ISIN], inst)
	}

	// ID -> Token
	m.idToToken[inst.ID] = inst.InstrumentToken

	// Get the exchange token out of the instrument and add it to
	// the segment name -> ID map.
	seg := inst.Exchange
	if inst.Segment == segIndices {
		seg = inst.Segment
	}
	if _, ok := m.segmentIDs[seg]; !ok {
		m.segmentIDs[seg] = GetSegmentID(inst.InstrumentToken)
	}

	// ID -> Instrument
	m.idToInst[inst.ID] = inst

	// segment:tradingsymbol
	// (to cover indices that are mapped by segments)
	// and not exchanges always.
	if inst.Segment == segIndices {
		m.idToInst[inst.Segment+":"+inst.Tradingsymbol] = inst
	}

	m.tokenToInstrument[inst.InstrumentToken] = inst
}

// Count returns the number of instruments loaded.
func (m *Manager) Count() int {
	return len(m.tokenToInstrument)
}

// GetSegmentID returns the segment ID for the instrument token.
func GetSegmentID(instToken uint32) uint32 {
	return instToken & 0xFF
}

// ExchTokenToInstToken converts an exchange token to an instrument token.
func ExchTokenToInstToken(segID, exchToken uint32) uint32 {
	return (exchToken << 8) + segID
}
