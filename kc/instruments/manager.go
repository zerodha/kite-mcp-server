package instruments

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultInstrumentsURL = "https://api.kite.trade/instruments.json"
	segIndices            = "INDICES"
	defaultUpdateHour     = 8 // 8 AM IST
	defaultUpdateMinute   = 0 // 0 minutes
	defaultRetryAttempts  = 3
	defaultRetryDelay     = 3 * time.Second
)

var (
	// instrumentsURL can be overridden for testing
	instrumentsURL = defaultInstrumentsURL
)

var (
	// ErrInstrumentNotFound is returned when instrument was not found in the
	// loaded map.
	ErrInstrumentNotFound = errors.New("instrument not found")

	// ErrSegmentNotFound is returned when segment was not found in the
	// loaded map.
	ErrSegmentNotFound = errors.New("instrument segment not found")
)

// UpdateConfig holds configuration for instrument updates
type UpdateConfig struct {
	// UpdateHour is the hour in IST when instruments should be updated (0-23)
	UpdateHour int
	// UpdateMinute is the minute when instruments should be updated (0-59)
	UpdateMinute int
	// RetryAttempts is the number of retry attempts for failed updates
	RetryAttempts int
	// RetryDelay is the delay between retry attempts
	RetryDelay time.Duration
	// EnableScheduler enables automatic scheduled updates
	EnableScheduler bool
	// MemoryLimit is the maximum memory usage in bytes (0 = no limit)
	MemoryLimit int64
}

// DefaultUpdateConfig returns the default update configuration
func DefaultUpdateConfig() *UpdateConfig {
	return &UpdateConfig{
		UpdateHour:      defaultUpdateHour,
		UpdateMinute:    defaultUpdateMinute,
		RetryAttempts:   defaultRetryAttempts,
		RetryDelay:      defaultRetryDelay,
		EnableScheduler: true,
		MemoryLimit:     0, // No limit by default
	}
}

// UpdateStats holds statistics about instrument updates
type UpdateStats struct {
	LastUpdateTime      time.Time
	LastUpdateCount     int
	TotalUpdates        int
	FailedUpdates       int
	MemoryUsageBytes    int64
	ScheduledNextUpdate time.Time
}

// Manager provides thread-safe access to instrument data.
// All public methods are thread-safe and can be called concurrently.
type Manager struct {
	// Core instrument maps - protected by mutex
	isinToInstruments map[string][]*Instrument
	idToInst          map[string]*Instrument
	idToToken         map[string]uint32
	tokenToInstrument map[uint32]*Instrument

	// NSE=1, BSE=2 etc. This is extracted from instrument tokens
	// as they're loaded.
	segmentIDs map[string]uint32

	lastUpdated time.Time

	// Configuration and scheduling
	config *UpdateConfig
	stats  UpdateStats

	// Scheduler control
	schedulerCtx    context.Context
	schedulerCancel context.CancelFunc
	schedulerDone   chan struct{}

	// Logger for this manager
	logger *slog.Logger

	// mutex protects all map fields above
	// Read operations use RLock, write operations use Lock
	mutex sync.RWMutex
}

// Config holds configuration for creating a new instruments manager
type Config struct {
	UpdateConfig *UpdateConfig          // defaults to DefaultUpdateConfig() if nil
	Logger       *slog.Logger           // required
	TestData     map[uint32]*Instrument // if set, skips HTTP loading and uses test data
}

// New creates a new instruments manager with the given configuration
// If TestData is provided, the manager will use test data instead of loading from HTTP
func New(cfg Config) (*Manager, error) {
	if cfg.UpdateConfig == nil {
		cfg.UpdateConfig = DefaultUpdateConfig()
	}

	manager := newManagerWithConfig(cfg.UpdateConfig, cfg.Logger)

	if cfg.TestData != nil {
		// Test mode - load test data
		manager.LoadMap(cfg.TestData)
	} else {
		// Production mode - load from HTTP
		if err := manager.UpdateInstruments(); err != nil {
			return nil, fmt.Errorf("failed to load initial data: %w", err)
		}
	}

	return manager, nil
}

// NewManager creates a new instruments manager that loads from the API
// Deprecated: Use New(Config{Logger: logger}) instead
func NewManager(logger *slog.Logger) *Manager {
	return NewManagerWithConfig(DefaultUpdateConfig(), logger)
}

// NewManagerWithConfig creates a new instruments manager with the specified configuration
// Does not load instruments automatically - call LoadInitialData() to load data
// Deprecated: Use New(Config{UpdateConfig: config, Logger: logger}) instead
func NewManagerWithConfig(config *UpdateConfig, logger *slog.Logger) *Manager {
	return newManagerWithConfig(config, logger)
}

// newManagerWithConfig is the internal constructor used by both old and new APIs
func newManagerWithConfig(config *UpdateConfig, logger *slog.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		isinToInstruments: make(map[string][]*Instrument),
		idToInst:          make(map[string]*Instrument),
		idToToken:         make(map[string]uint32),
		tokenToInstrument: make(map[uint32]*Instrument),
		segmentIDs:        make(map[string]uint32),
		lastUpdated:       time.Now(),
		config:            config,
		logger:            logger,
		schedulerCtx:      ctx,
		schedulerCancel:   cancel,
		schedulerDone:     make(chan struct{}),
	}

	// Start scheduler if enabled
	if config.EnableScheduler {
		go m.startScheduler()
	} else {
		// Close the done channel immediately if scheduler is not enabled
		close(m.schedulerDone)
	}

	return m
}

// LoadInitialData loads instruments data and should be called after manager creation
func (m *Manager) LoadInitialData() error {
	if err := m.UpdateInstruments(); err != nil {
		m.logger.Error("Error loading initial instruments data", "error", err)
		return err
	}
	return nil
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

// UpdateInstruments fetches instruments from Kite and updates the internal
// instruments data. The first call in a day will fetch instruments from the
// Kite API; subsequent calls within the same day will have no effect.
func (m *Manager) UpdateInstruments() error {
	return m.updateInstrumentsWithRetry(false)
}

// ForceUpdateInstruments forces an instrument update regardless of when it was last updated
func (m *Manager) ForceUpdateInstruments() error {
	return m.updateInstrumentsWithRetry(true)
}

// updateInstrumentsWithRetry performs the actual update with retry logic
func (m *Manager) updateInstrumentsWithRetry(force bool) error {
	var lastErr error

	// Get config with lock protection
	m.mutex.RLock()
	maxAttempts := m.config.RetryAttempts
	retryDelay := m.config.RetryDelay
	m.mutex.RUnlock()

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			m.logger.Warn("Retrying instrument update", "attempt", attempt+1, "max_attempts", maxAttempts, "delay", retryDelay)
			time.Sleep(retryDelay)
		}

		count, err := m.updateInstruments(force)
		if err != nil {
			lastErr = err
			m.updateStats(false, count)
			m.logger.Error("Instrument update failed", "attempt", attempt+1, "error", err)
			continue
		}

		// Success
		m.updateStats(true, count)
		return nil
	}

	return fmt.Errorf("instrument update failed after %d attempts: %v", maxAttempts, lastErr)
}

// updateInstruments performs the actual instrument update logic
func (m *Manager) updateInstruments(force bool) (int, error) {
	// Check if we need to update - using a read lock
	m.mutex.RLock()
	count := len(m.tokenToInstrument)
	lastUpdated := m.lastUpdated
	m.mutex.RUnlock()

	// Don't update if we already have instruments loaded today (unless forced)
	if !force && count > 0 && !isPreviousDayIST(lastUpdated) {
		m.logger.Info("Instruments already loaded today. Skipping update.", "count", count)
		return count, nil
	}

	m.logger.Info("Updating instruments...", "force", force)

	// Load instruments from Kite API
	m.logger.Info("Loading instruments from Kite API URL")
	instruments, err := m.loadFromURL()
	if err != nil {
		m.logger.Error("Error loading from URL", "error", err)
		return 0, fmt.Errorf("error loading from URL: %v", err)
	}

	// Create temporary maps instead of modifying existing maps under a lock
	isinToInstruments := make(map[string][]*Instrument)
	idToInst := make(map[string]*Instrument)
	idToToken := make(map[string]uint32)
	tokenToInstrument := make(map[uint32]*Instrument)
	segmentIDs := make(map[string]uint32)

	// Process all instruments without holding a lock
	m.logger.Info("Processing instruments", "count", len(instruments))
	for _, inst := range instruments {
		// ISIN -> Instrument
		if inst.ISIN != "" {
			if _, ok := isinToInstruments[inst.ISIN]; !ok {
				isinToInstruments[inst.ISIN] = []*Instrument{}
			}
			isinToInstruments[inst.ISIN] = append(isinToInstruments[inst.ISIN], inst)
		}

		// ID -> Token
		idToToken[inst.ID] = inst.InstrumentToken

		// Get the exchange token out of the instrument and add it to
		// the segment name -> ID map.
		seg := inst.Exchange
		if inst.Segment == segIndices {
			seg = inst.Segment
		}
		if _, ok := segmentIDs[seg]; !ok {
			segmentIDs[seg] = GetSegmentID(inst.InstrumentToken)
		}

		// ID -> Instrument
		idToInst[inst.ID] = inst

		// segment:tradingsymbol
		// (to cover indices that are mapped by segments)
		// and not exchanges always.
		if inst.Segment == segIndices {
			idToInst[inst.Segment+":"+inst.Tradingsymbol] = inst
		}

		tokenToInstrument[inst.InstrumentToken] = inst
	}

	// Now that all processing is done, acquire the lock to update the maps
	m.mutex.Lock()
	m.isinToInstruments = isinToInstruments
	m.idToInst = idToInst
	m.idToToken = idToToken
	m.tokenToInstrument = tokenToInstrument
	m.segmentIDs = segmentIDs
	m.lastUpdated = time.Now()
	instrumentCount := len(tokenToInstrument)
	m.mutex.Unlock()

	m.logger.Info("Loaded instruments", "count", instrumentCount)
	return instrumentCount, nil
}

func (m *Manager) loadFromURL() (map[uint32]*Instrument, error) {
	m.logger.Debug("Creating HTTP request to fetch instruments", "url", instrumentsURL)
	// HTTP GET request to instruments URL
	req, err := http.NewRequest("GET", instrumentsURL, nil)
	if err != nil {
		m.logger.Error("Error creating HTTP request", "error", err)
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	// Add compression header
	req.Header.Add("Accept-Encoding", "gzip")
	m.logger.Debug("Added gzip Accept-Encoding header")

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	m.logger.Debug("Created HTTP client with timeout", "timeout", "30s")

	// Execute request
	m.logger.Debug("Sending HTTP request to fetch instruments")
	resp, err := client.Do(req)
	if err != nil {
		m.logger.Error("HTTP request failed", "error", err)
		return nil, fmt.Errorf("error fetching instruments: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	m.logger.Debug("Received HTTP response",
		"status", resp.StatusCode,
		"content-length", resp.Header.Get("Content-Length"),
		"content-encoding", resp.Header.Get("Content-Encoding"))

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		m.logger.Debug("Response is gzip compressed, creating gzip reader")
		gzipReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			m.logger.Error("Failed to create gzip reader", "error", err)
			return nil, fmt.Errorf("error creating gzip reader: %v", err)
		}
		defer func() { _ = gzipReader.Close() }()
		reader = gzipReader
	}

	m.logger.Debug("Starting to parse instruments JSON")
	return m.parseInstrumentsJSON(reader)
}

func (m *Manager) parseInstrumentsJSON(reader io.Reader) (map[uint32]*Instrument, error) {
	m.logger.Debug("Creating instrument map")
	mp := make(map[uint32]*Instrument)

	// Process JSONL formatted data
	m.logger.Debug("Creating scanner for JSONL data")
	scanner := bufio.NewScanner(reader)

	// Increase scanner buffer to handle large lines
	const maxCapacity = 512 * 1024 // 512KB
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)
	m.logger.Debug("Scanner buffer size increased", "size_bytes", maxCapacity)

	count := 0
	m.logger.Debug("Starting to scan JSON lines")
	// Read and process each line as a separate JSON object
	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines
		if len(strings.TrimSpace(line)) == 0 {
			continue
		}

		var instrument Instrument
		if err := json.Unmarshal([]byte(line), &instrument); err != nil {
			m.logger.Error("JSON unmarshal error", "error", err, "line", line)
			return nil, fmt.Errorf("error parsing instrument JSON: %v (line: %s)", err, line)
		}

		// Process each instrument
		mp[instrument.InstrumentToken] = &instrument

		count++
		if count%10000 == 0 {
			m.logger.Debug("Processing instruments progress", "count", count)
		}
	}

	if err := scanner.Err(); err != nil {
		m.logger.Error("Scanner error", "error", err)
		return nil, fmt.Errorf("error reading instruments file: %v", err)
	}

	m.logger.Debug("Successfully parsed instruments", "count", len(mp))
	return mp, nil
}

// LoadMap from tokenToInstrument map to the manager.
// This method is thread-safe and acquires its own write lock.
func (m *Manager) LoadMap(tokenToInstrument map[uint32]*Instrument) {
	m.logger.Debug("LoadMap: loading instruments", "count", len(tokenToInstrument))

	m.mutex.Lock()
	defer m.mutex.Unlock()

	count := 0
	batchSize := 5000

	for _, inst := range tokenToInstrument {
		m.insertUnsafe(inst)
		count++
		if count%batchSize == 0 {
			m.logger.Debug("LoadMap: progress", "inserted", count, "total", len(tokenToInstrument))
		}
	}
	m.logger.Debug("LoadMap: completed", "count", count)
}

// Insert inserts a new instrument.
// This method is thread-safe and acquires its own write lock.
func (m *Manager) Insert(inst *Instrument) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.insertUnsafe(inst)
}

// insertUnsafe inserts a new instrument without acquiring locks.
// This method must be called with the mutex already held.
func (m *Manager) insertUnsafe(inst *Instrument) {
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
	m.mutex.RLock()
	count := len(m.tokenToInstrument)
	m.mutex.RUnlock()
	return count
}

// GetUpdateStats returns current update statistics
func (m *Manager) GetUpdateStats() UpdateStats {
	m.mutex.RLock()
	stats := m.stats
	stats.ScheduledNextUpdate = m.getNextScheduledUpdate()
	m.mutex.RUnlock()
	return stats
}

// GetConfig returns the current configuration
func (m *Manager) GetConfig() *UpdateConfig {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.config
}

// UpdateConfig updates the manager configuration
func (m *Manager) UpdateConfig(config *UpdateConfig) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.config = config
	m.logger.Info("Instruments manager configuration updated",
		"update_hour", config.UpdateHour,
		"update_minute", config.UpdateMinute,
		"retry_attempts", config.RetryAttempts,
		"scheduler_enabled", config.EnableScheduler)
}

// Shutdown gracefully shuts down the instruments manager
func (m *Manager) Shutdown() {
	m.logger.Info("Shutting down instruments manager...")
	if m.schedulerCancel != nil {
		m.schedulerCancel()
	}
	if m.schedulerDone != nil {
		<-m.schedulerDone
	}
	m.logger.Info("Instruments manager shutdown complete")
}

// updateStats updates internal statistics
func (m *Manager) updateStats(success bool, count int) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.stats.TotalUpdates++
	if !success {
		m.stats.FailedUpdates++
	} else {
		m.stats.LastUpdateTime = time.Now()
		m.stats.LastUpdateCount = count
	}
}

// startScheduler starts the background scheduler for automatic updates
func (m *Manager) startScheduler() {
	defer close(m.schedulerDone)

	// Safely read config for initial logging
	m.mutex.RLock()
	updateHour := m.config.UpdateHour
	updateMinute := m.config.UpdateMinute
	m.mutex.RUnlock()

	m.logger.Info("Starting instruments update scheduler",
		"update_time", fmt.Sprintf("%02d:%02d IST", updateHour, updateMinute))

	ticker := time.NewTicker(5 * time.Minute) // Check every 5 minutes
	defer ticker.Stop()

	for {
		select {
		case <-m.schedulerCtx.Done():
			m.logger.Info("Instruments scheduler stopped")
			return

		case <-ticker.C:
			if m.shouldUpdate() {
				m.logger.Info("Starting scheduled instrument update")
				if err := m.ForceUpdateInstruments(); err != nil {
					m.logger.Error("Scheduled instrument update failed", "error", err)
				} else {
					m.logger.Info("Scheduled instrument update completed successfully")
				}
			}
		}
	}
}

// shouldUpdate checks if it's time for a scheduled update
func (m *Manager) shouldUpdate() bool {
	now := time.Now()
	ist, _ := time.LoadLocation("Asia/Kolkata")
	nowIST := now.In(ist)

	// Safely read config values
	m.mutex.RLock()
	updateHour := m.config.UpdateHour
	updateMinute := m.config.UpdateMinute
	m.mutex.RUnlock()

	// Check if it's the right time (hour and minute)
	if nowIST.Hour() != updateHour || nowIST.Minute() != updateMinute {
		return false
	}

	// Check if we already updated today
	m.mutex.RLock()
	lastUpdated := m.stats.LastUpdateTime
	m.mutex.RUnlock()

	if !lastUpdated.IsZero() {
		lastUpdatedIST := lastUpdated.In(ist)

		// If last update was today, don't update again
		if nowIST.Year() == lastUpdatedIST.Year() &&
			nowIST.YearDay() == lastUpdatedIST.YearDay() {
			return false
		}
	}

	return true
}

// getNextScheduledUpdate calculates the next scheduled update time
func (m *Manager) getNextScheduledUpdate() time.Time {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Now().In(ist)

	// Get config with lock protection (this method is called while holding RLock already)
	// So we don't need additional locking here as it's called from GetUpdateStats which already has RLock
	nextUpdate := time.Date(now.Year(), now.Month(), now.Day(),
		m.config.UpdateHour, m.config.UpdateMinute, 0, 0, ist)

	// If the time has already passed today, schedule for tomorrow
	if nextUpdate.Before(now) {
		nextUpdate = nextUpdate.Add(24 * time.Hour)
	}

	return nextUpdate
}

// GetSegmentID returns the segment ID for the instrument token.
func GetSegmentID(instToken uint32) uint32 {
	return instToken & 0xFF
}

// ExchTokenToInstToken converts an exchange token to an instrument token.
func ExchTokenToInstToken(segID, exchToken uint32) uint32 {
	return (exchToken << 8) + segID
}
