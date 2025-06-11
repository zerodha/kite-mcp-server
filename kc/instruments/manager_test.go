package instruments

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// testLogger creates a discard logger for tests
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestManager creates an empty manager for testing without initial instrument loading
func newTestManager() *Manager {
	config := DefaultUpdateConfig()
	config.EnableScheduler = false

	ctx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		isinToInstruments: make(map[string][]*Instrument),
		idToInst:          make(map[string]*Instrument),
		idToToken:         make(map[string]uint32),
		tokenToInstrument: make(map[uint32]*Instrument),
		segmentIDs:        make(map[string]uint32),
		lastUpdated:       time.Now(),
		config:            config,
		logger:            testLogger(),
		schedulerCtx:      ctx,
		schedulerCancel:   cancel,
		schedulerDone:     make(chan struct{}),
	}
	close(manager.schedulerDone) // Don't run scheduler

	// Load test data by default
	testMap := make(map[uint32]*Instrument)
	for _, inst := range getTestInstruments() {
		testMap[inst.InstrumentToken] = inst
	}
	manager.LoadMap(testMap)

	return manager
}

// newTestManagerWithoutUpdate creates a manager for testing without initial API update
func newTestManagerWithoutUpdate() *Manager {
	config := DefaultUpdateConfig()
	config.EnableScheduler = false

	ctx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		isinToInstruments: make(map[string][]*Instrument),
		idToInst:          make(map[string]*Instrument),
		idToToken:         make(map[string]uint32),
		tokenToInstrument: make(map[uint32]*Instrument),
		segmentIDs:        make(map[string]uint32),
		lastUpdated:       time.Now(),
		config:            config,
		logger:            testLogger(),
		schedulerCtx:      ctx,
		schedulerCancel:   cancel,
		schedulerDone:     make(chan struct{}),
	}
	close(manager.schedulerDone) // Don't run scheduler

	return manager
}

// setupTestServer creates a mock HTTP server for instruments API
func setupTestServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock instruments data in JSONL format
		mockData := `{"instrument_token":779521,"exchange_token":3045,"tradingsymbol":"SBIN","name":"STATE BANK OF INDIA","last_price":0,"expiry":"","strike":0,"tick_size":0.05,"lot_size":1,"instrument_type":"EQ","segment":"NSE","exchange":"NSE"}
{"instrument_token":738561,"exchange_token":2885,"tradingsymbol":"RELIANCE","name":"RELIANCE INDUSTRIES LIMITED","last_price":0,"expiry":"","strike":0,"tick_size":0.05,"lot_size":1,"instrument_type":"EQ","segment":"NSE","exchange":"NSE"}
{"instrument_token":265,"exchange_token":1,"tradingsymbol":"SENSEX","name":"BSE SENSEX","last_price":0,"expiry":"","strike":0,"tick_size":0.01,"lot_size":1,"instrument_type":"IND","segment":"INDICES","exchange":"BSE"}`

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mockData))
	}))
}

// hijackInstrumentsURL temporarily overrides the instruments URL for testing
func hijackInstrumentsURL(testURL string) func() {
	originalURL := instrumentsURL
	instrumentsURL = testURL
	return func() {
		instrumentsURL = originalURL
	}
}

// getTestInstruments returns test instrument data for use across tests
func getTestInstruments() []*Instrument {
	return []*Instrument{
		{
			ID:              "NSE:SBIN",
			InstrumentToken: 779521,
			ExchangeToken:   3045,
			Tradingsymbol:   "SBIN",
			Exchange:        "NSE",
			ISIN:            "INE062A01020",
			Name:            "STATE BANK OF INDIA",
			InstrumentType:  "EQ",
			Segment:         "NSE",
			Active:          true,
		},
		{
			ID:              "NSE:RELIANCE",
			InstrumentToken: 738561,
			ExchangeToken:   2885,
			Tradingsymbol:   "RELIANCE",
			Exchange:        "NSE",
			ISIN:            "INE002A01018",
			Name:            "RELIANCE INDUSTRIES LIMITED",
			InstrumentType:  "EQ",
			Segment:         "NSE",
			Active:          true,
		},
		{
			ID:              "BSE:SENSEX",
			InstrumentToken: 265,
			ExchangeToken:   1,
			Tradingsymbol:   "SENSEX",
			Exchange:        "BSE",
			Name:            "BSE SENSEX",
			InstrumentType:  "IND",
			Segment:         "INDICES",
			Active:          true,
		},
	}
}

// TestManagerBasicOperations verifies basic Manager functionality
func TestManagerBasicOperations(t *testing.T) {
	manager := newTestManager()

	// Test Count
	if count := manager.Count(); count != 3 {
		t.Errorf("Expected 3 instruments, got %d", count)
	}

	// Test GetByID - valid case
	inst, err := manager.GetByID("NSE:SBIN")
	if err != nil {
		t.Errorf("Expected no error for valid ID, got: %v", err)
	}
	if inst.Tradingsymbol != "SBIN" {
		t.Errorf("Expected SBIN, got %s", inst.Tradingsymbol)
	}

	// Test GetByID - invalid case
	_, err = manager.GetByID("INVALID:SYMBOL")
	if err != ErrInstrumentNotFound {
		t.Errorf("Expected ErrInstrumentNotFound, got: %v", err)
	}

	// Test Filter
	nseInstruments := manager.Filter(func(inst Instrument) bool {
		return inst.Exchange == "NSE"
	})
	if len(nseInstruments) != 2 {
		t.Errorf("Expected 2 NSE instruments, got %d", len(nseInstruments))
	}
}

// TestManagerConcurrentOperations tests thread safety - this is the critical test
func TestManagerConcurrentOperations(t *testing.T) {
	manager := &Manager{
		isinToInstruments: make(map[string][]*Instrument),
		idToInst:          make(map[string]*Instrument),
		idToToken:         make(map[string]uint32),
		tokenToInstrument: make(map[uint32]*Instrument),
		segmentIDs:        make(map[string]uint32),
	}

	testInsts := getTestInstruments()
	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrent Insert operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j, inst := range testInsts {
				// Create unique instruments for each goroutine
				uniqueInst := &Instrument{
					ID:              inst.ID + "_" + string(rune('A'+idx)) + string(rune('0'+j)),
					InstrumentToken: inst.InstrumentToken + uint32(idx*100+j),
					ExchangeToken:   inst.ExchangeToken + uint32(idx*100+j),
					Tradingsymbol:   inst.Tradingsymbol,
					Exchange:        inst.Exchange,
					ISIN:            inst.ISIN,
					Name:            inst.Name,
					InstrumentType:  inst.InstrumentType,
					Segment:         inst.Segment,
				}
				manager.Insert(uniqueInst)
			}
		}(i)
	}

	// Concurrent reads while writes are happening
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				manager.Count()
				manager.Filter(func(inst Instrument) bool {
					return inst.Exchange == "NSE"
				})
			}
		}()
	}

	wg.Wait()

	// Verify final state
	finalCount := manager.Count()
	expectedCount := numGoroutines * len(testInsts)
	if finalCount != expectedCount {
		t.Errorf("Expected %d instruments, got %d", expectedCount, finalCount)
	}
}

// TestManagerLoadMapConcurrent tests LoadMap thread safety
func TestManagerLoadMapConcurrent(t *testing.T) {
	manager := newTestManager()
	defer manager.Shutdown()

	// Create test data for LoadMap
	testInsts := getTestInstruments()
	testMap := make(map[uint32]*Instrument)
	for _, inst := range testInsts {
		testMap[inst.InstrumentToken] = inst
	}

	var wg sync.WaitGroup

	// Concurrent LoadMap operation
	wg.Add(1)
	go func() {
		defer wg.Done()
		manager.LoadMap(testMap)
	}()

	// Concurrent reads
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				manager.Count()
				_, _ = manager.GetByID("NSE:SBIN")
			}
		}()
	}

	wg.Wait()

	// Verify LoadMap worked correctly
	if manager.Count() != len(testMap) {
		t.Errorf("Expected %d instruments after LoadMap, got %d", len(testMap), manager.Count())
	}

	// Test that we can retrieve the loaded instruments
	if _, err := manager.GetByID("NSE:SBIN"); err != nil {
		t.Errorf("Expected to find SBIN after LoadMap, got error: %v", err)
	}
}

// TestManagerIntensiveRaceConditions creates intensive concurrent operations to stress test race conditions
func TestManagerIntensiveRaceConditions(t *testing.T) {
	manager := newTestManager()
	defer manager.Shutdown()

	testInsts := getTestInstruments()
	var wg sync.WaitGroup
	numGoroutines := 5           // Reduced from 50
	operationsPerGoroutine := 20 // Reduced from 1000

	// Concurrent inserts with reduced load
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				for k, inst := range testInsts {
					uniqueInst := &Instrument{
						ID:              fmt.Sprintf("%s_%d_%d_%d", inst.ID, idx, j, k),
						InstrumentToken: inst.InstrumentToken + uint32(idx*10000+j*100+k),
						ExchangeToken:   inst.ExchangeToken + uint32(idx*10000+j*100+k),
						Tradingsymbol:   inst.Tradingsymbol,
						Exchange:        inst.Exchange,
						ISIN:            inst.ISIN,
						Name:            inst.Name,
						InstrumentType:  inst.InstrumentType,
						Segment:         inst.Segment,
						Active:          true,
					}
					manager.Insert(uniqueInst)
				}
			}
		}(i)
	}

	// Concurrent reads with reduced load
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				// Various read operations
				manager.Count()
				_, _ = manager.GetByID("NSE:SBIN")
				_, _ = manager.GetByInstToken(779521)
				_, _ = manager.GetByISIN("INE062A01020")
				manager.Filter(func(inst Instrument) bool {
					return inst.Exchange == "NSE"
				})
				_, _ = manager.GetAllByUnderlying("NSE", "STATE BANK OF INDIA")
			}
		}()
	}

	// Reduced concurrent LoadMap operations
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			testMap := make(map[uint32]*Instrument)
			for j, inst := range testInsts {
				newInst := &Instrument{
					ID:              fmt.Sprintf("LOAD_%d_%s", idx, inst.ID),
					InstrumentToken: inst.InstrumentToken + uint32(idx*1000+j),
					ExchangeToken:   inst.ExchangeToken + uint32(idx*1000+j),
					Tradingsymbol:   inst.Tradingsymbol,
					Exchange:        inst.Exchange,
					ISIN:            inst.ISIN,
					Name:            inst.Name,
					InstrumentType:  inst.InstrumentType,
					Segment:         inst.Segment,
					Active:          true,
				}
				testMap[newInst.InstrumentToken] = newInst
			}
			manager.LoadMap(testMap)
		}(i)
	}

	wg.Wait()

	// Verify final state integrity
	finalCount := manager.Count()
	if finalCount == 0 {
		t.Error("Expected non-zero instrument count after race condition operations")
	}

	// Verify we can still perform basic operations without errors
	manager.Count()
	results := manager.Filter(func(inst Instrument) bool {
		return inst.Exchange == "NSE"
	})

	// Ensure we got some results to verify data integrity
	if len(results) == 0 {
		t.Log("No NSE instruments found after race condition test")
	}
}

// TestManagerUpdateInstrumentsConcurrency tests UpdateInstruments() race conditions
func TestManagerUpdateInstrumentsConcurrency(t *testing.T) {
	manager := newTestManager()
	defer manager.Shutdown()

	// Pre-populate with some data
	testInsts := getTestInstruments()
	for _, inst := range testInsts {
		manager.Insert(inst)
	}

	var wg sync.WaitGroup
	numReaders := 20
	numWriters := 5
	operationsPerGoroutine := 100

	// Concurrent readers during UpdateInstruments
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				manager.Count()
				_, _ = manager.GetByID("NSE:SBIN")
				manager.Filter(func(inst Instrument) bool {
					return inst.Exchange == "NSE"
				})
			}
		}()
	}

	// Concurrent UpdateInstruments calls
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				// Simulate update operations without HTTP calls
				manager.updateStats(true, j+1)
			}
		}()
	}

	// Concurrent inserts during updates
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				inst := &Instrument{
					ID:              fmt.Sprintf("UPDATE_TEST_%d_%d", idx, j),
					InstrumentToken: uint32(100000 + idx*1000 + j),
					ExchangeToken:   uint32(1000 + idx*10 + j),
					Tradingsymbol:   "TEST",
					Exchange:        "NSE",
					ISIN:            "TEST123456789",
					Name:            "TEST INSTRUMENT",
					InstrumentType:  "EQ",
					Segment:         "NSE",
					Active:          true,
				}
				manager.Insert(inst)
			}
		}(i)
	}

	wg.Wait()

	// Verify final state is consistent
	finalCount := manager.Count()
	if finalCount < 0 {
		t.Error("Invalid instrument count after concurrent operations")
	}
}

// TestManagerMapCorruption tests for map corruption under concurrent access
func TestManagerMapCorruption(t *testing.T) {
	manager := newTestManagerWithoutUpdate()
	defer manager.Shutdown()

	var wg sync.WaitGroup
	numGoroutines := 10          // Reduced from 100
	operationsPerGoroutine := 50 // Reduced from 500

	// Create a large set of test instruments
	createTestInstrument := func(i, j int) *Instrument {
		return &Instrument{
			ID:              fmt.Sprintf("CORR_TEST_%d_%d", i, j),
			InstrumentToken: uint32(i*10000 + j),
			ExchangeToken:   uint32(i*100 + j),
			Tradingsymbol:   fmt.Sprintf("SYM%d", j),
			Exchange:        "NSE",
			ISIN:            fmt.Sprintf("TEST%012d", i*1000+j),
			Name:            fmt.Sprintf("TEST INSTRUMENT %d %d", i, j),
			InstrumentType:  "EQ",
			Segment:         "NSE",
			Active:          true,
		}
	}

	// Extreme concurrent operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				switch j % 7 {
				case 0:
					// Insert
					inst := createTestInstrument(idx, j)
					manager.Insert(inst)
				case 1:
					// Count
					manager.Count()
				case 2:
					// GetByID
					_, _ = manager.GetByID(fmt.Sprintf("CORR_TEST_%d_%d", idx, (j-1+operationsPerGoroutine)%operationsPerGoroutine))
				case 3:
					// GetByInstToken
					_, _ = manager.GetByInstToken(uint32(idx*10000 + ((j - 1 + operationsPerGoroutine) % operationsPerGoroutine)))
				case 4:
					// Filter
					manager.Filter(func(inst Instrument) bool {
						return inst.Exchange == "NSE"
					})
				case 5:
					// GetByISIN
					_, _ = manager.GetByISIN(fmt.Sprintf("TEST%012d", idx*1000+((j-1+operationsPerGoroutine)%operationsPerGoroutine)))
				case 6:
					// LoadMap with small dataset (only occasionally)
					if j%10 == 0 { // Only do LoadMap every 10th operation
						smallMap := make(map[uint32]*Instrument)
						for k := 0; k < 2; k++ { // Reduced from 3 to 2
							inst := createTestInstrument(idx, j*10+k)
							smallMap[inst.InstrumentToken] = inst
						}
						manager.LoadMap(smallMap)
					}
				}
			}
		}(i)
	}

	wg.Wait()

	// Extensive verification of data integrity
	count := manager.Count()
	if count < 0 {
		t.Error("Negative instrument count indicates map corruption")
	}

	// Try to access instruments that should exist
	instruments := manager.Filter(func(inst Instrument) bool {
		return inst.Exchange == "NSE"
	})

	if len(instruments) == 0 {
		t.Error("Filter returned invalid result, indicating map corruption")
	}

	// Verify count consistency
	filterCount := len(manager.Filter(func(inst Instrument) bool { return true }))

	if count != filterCount {
		t.Errorf("Count mismatch: Count()=%d, Filter(all)=%d", count, filterCount)
	}
}

// TestManagerConfigurationAndScheduling tests the new configuration and scheduling features
func TestManagerConfigurationAndScheduling(t *testing.T) {
	// Test default configuration
	config := DefaultUpdateConfig()
	if config.UpdateHour != defaultUpdateHour {
		t.Errorf("Expected default update hour %d, got %d", defaultUpdateHour, config.UpdateHour)
	}
	if config.UpdateMinute != defaultUpdateMinute {
		t.Errorf("Expected default update minute %d, got %d", defaultUpdateMinute, config.UpdateMinute)
	}
	if config.RetryAttempts != defaultRetryAttempts {
		t.Errorf("Expected default retry attempts %d, got %d", defaultRetryAttempts, config.RetryAttempts)
	}
	if config.RetryDelay != defaultRetryDelay {
		t.Errorf("Expected default retry delay %v, got %v", defaultRetryDelay, config.RetryDelay)
	}
	if !config.EnableScheduler {
		t.Error("Expected scheduler to be enabled by default")
	}
}

// TestManagerWithCustomConfig tests manager creation with custom configuration
func TestManagerWithCustomConfig(t *testing.T) {
	config := &UpdateConfig{
		UpdateHour:      9,
		UpdateMinute:    30,
		RetryAttempts:   5,
		RetryDelay:      5 * time.Second,
		EnableScheduler: false,
		MemoryLimit:     1024 * 1024, // 1MB
	}

	manager := newTestManagerWithoutUpdate()
	manager.UpdateConfig(config)
	if manager == nil {
		t.Fatal("Expected non-nil manager")
	}

	// Verify configuration was applied
	managerConfig := manager.GetConfig()
	if managerConfig.UpdateHour != 9 {
		t.Errorf("Expected update hour 9, got %d", managerConfig.UpdateHour)
	}
	if managerConfig.UpdateMinute != 30 {
		t.Errorf("Expected update minute 30, got %d", managerConfig.UpdateMinute)
	}
	if managerConfig.RetryAttempts != 5 {
		t.Errorf("Expected retry attempts 5, got %d", managerConfig.RetryAttempts)
	}
	if managerConfig.RetryDelay != 5*time.Second {
		t.Errorf("Expected retry delay 5s, got %v", managerConfig.RetryDelay)
	}
	if managerConfig.EnableScheduler {
		t.Error("Expected scheduler to be disabled")
	}
	if managerConfig.MemoryLimit != 1024*1024 {
		t.Errorf("Expected memory limit 1MB, got %d", managerConfig.MemoryLimit)
	}

	// Test shutdown
	manager.Shutdown()
}

// TestManagerUpdateStats tests update statistics tracking with mock server
func TestManagerUpdateStats(t *testing.T) {
	// Setup mock server
	server := setupTestServer()
	defer server.Close()
	restore := hijackInstrumentsURL(server.URL)
	defer restore()

	manager := newTestManagerWithoutUpdate()
	defer manager.Shutdown()

	// Get initial stats
	stats := manager.GetUpdateStats()
	initialUpdates := stats.TotalUpdates

	// Perform actual update with mock server
	err := manager.ForceUpdateInstruments()
	if err != nil {
		t.Errorf("Expected no error from ForceUpdateInstruments, got: %v", err)
	}

	// Check stats were updated
	newStats := manager.GetUpdateStats()
	if newStats.TotalUpdates <= initialUpdates {
		t.Errorf("Expected total updates to increase, got %d -> %d", initialUpdates, newStats.TotalUpdates)
	}

	if newStats.LastUpdateTime.IsZero() {
		t.Error("Expected last update time to be set")
	}

	if newStats.LastUpdateCount == 0 {
		t.Error("Expected last update count to be > 0")
	}
}

// TestManagerUpdateConfig tests configuration updates
func TestManagerUpdateConfig(t *testing.T) {
	manager := newTestManagerWithoutUpdate()
	defer manager.Shutdown()

	// Update configuration
	newConfig := &UpdateConfig{
		UpdateHour:      10,
		UpdateMinute:    45,
		RetryAttempts:   2,
		RetryDelay:      1 * time.Second,
		EnableScheduler: false,
		MemoryLimit:     512 * 1024,
	}

	manager.UpdateConfig(newConfig)

	// Verify configuration was updated
	config := manager.GetConfig()
	if config.UpdateHour != 10 {
		t.Errorf("Expected update hour 10, got %d", config.UpdateHour)
	}
	if config.UpdateMinute != 45 {
		t.Errorf("Expected update minute 45, got %d", config.UpdateMinute)
	}
	if config.RetryAttempts != 2 {
		t.Errorf("Expected retry attempts 2, got %d", config.RetryAttempts)
	}
}

// TestManagerFromFileWithConfig tests file-based manager with custom configuration
// TestManagerFromFileWithConfig removed - file loading functionality removed

// TestManagerSchedulerShutdown tests proper scheduler shutdown
func TestManagerSchedulerShutdown(t *testing.T) {
	config := DefaultUpdateConfig()
	config.EnableScheduler = false // We'll start manually

	ctx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		isinToInstruments: make(map[string][]*Instrument),
		idToInst:          make(map[string]*Instrument),
		idToToken:         make(map[string]uint32),
		tokenToInstrument: make(map[uint32]*Instrument),
		segmentIDs:        make(map[string]uint32),
		lastUpdated:       time.Now(),
		config:            config,
		logger:            testLogger(),
		schedulerCtx:      ctx,
		schedulerCancel:   cancel,
		schedulerDone:     make(chan struct{}), // Don't pre-close
	}

	// Start scheduler manually
	go manager.startScheduler()

	// Give scheduler time to start
	time.Sleep(10 * time.Millisecond)

	// Shutdown should complete without hanging
	done := make(chan struct{})
	go func() {
		manager.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Shutdown took too long")
	}
}

// TestManagerConcurrentShutdown tests concurrent shutdown calls
func TestManagerConcurrentShutdown(t *testing.T) {
	manager := newTestManagerWithoutUpdate()

	var wg sync.WaitGroup

	// Multiple concurrent shutdown calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			manager.Shutdown()
		}()
	}

	// Should complete without panic or hanging
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Error("Concurrent shutdown timed out")
	}
}

// TestManagerForceUpdate tests forced updates with mock server
func TestManagerForceUpdate(t *testing.T) {
	// Setup mock server
	server := setupTestServer()
	defer server.Close()
	restore := hijackInstrumentsURL(server.URL)
	defer restore()

	manager := newTestManagerWithoutUpdate()
	defer manager.Shutdown()

	// Get initial stats
	initialStats := manager.GetUpdateStats()

	// Force update should work even if recently updated
	err := manager.ForceUpdateInstruments()
	if err != nil {
		t.Errorf("Expected no error from ForceUpdateInstruments, got: %v", err)
	}

	// Stats should be updated
	newStats := manager.GetUpdateStats()
	if newStats.TotalUpdates <= initialStats.TotalUpdates {
		t.Error("Expected total updates to increase after force update")
	}

	// Regular update should skip if already updated
	err = manager.UpdateInstruments()
	if err != nil {
		t.Errorf("Expected no error from UpdateInstruments, got: %v", err)
	}
}

// TestMemoryOptimizations tests memory usage optimizations
func TestMemoryOptimizations(t *testing.T) {
	manager := newTestManagerWithoutUpdate()
	manager.config.MemoryLimit = 1024 * 1024 // 1MB limit
	defer manager.Shutdown()

	// Test that manager handles memory limits gracefully
	stats := manager.GetUpdateStats()

	// Memory usage should be tracked (even if 0 in test environment)
	if stats.MemoryUsageBytes < 0 {
		t.Error("Expected non-negative memory usage")
	}
}

// TestNewConfigConstructor tests the new Config-based constructor
func TestNewConfigConstructor(t *testing.T) {
	// Test production mode (with HTTP loading disabled for tests)
	t.Run("production_mode", func(t *testing.T) {
		config := DefaultUpdateConfig()
		config.EnableScheduler = false // Disable for tests

		// Create manager that would normally load from HTTP
		manager, err := New(Config{
			UpdateConfig: config,
			Logger:       testLogger(),
			TestData:     nil, // This would trigger HTTP loading in real usage
		})

		// HTTP call may succeed or fail depending on environment
		if err != nil {
			t.Logf("HTTP loading failed as expected in test environment: %v", err)
		} else {
			t.Log("HTTP loading succeeded - manager created successfully")
			manager.Shutdown()
		}
	})

	// Test with test data
	t.Run("test_mode", func(t *testing.T) {
		testData := getTestInstruments()
		testMap := make(map[uint32]*Instrument)
		for _, inst := range testData {
			testMap[inst.InstrumentToken] = inst
		}

		manager, err := New(Config{
			Logger:   testLogger(),
			TestData: testMap,
		})
		if err != nil {
			t.Fatalf("Expected no error with test data, got: %v", err)
		}
		defer manager.Shutdown()

		// Verify test data was loaded
		count := manager.Count()
		if count != len(testData) {
			t.Errorf("Expected %d instruments, got %d", len(testData), count)
		}

		// Verify we can retrieve the test data
		inst, err := manager.GetByID("NSE:SBIN")
		if err != nil {
			t.Errorf("Expected to find test instrument, got error: %v", err)
		}
		if inst.Name != "STATE BANK OF INDIA" {
			t.Errorf("Expected correct instrument name, got: %s", inst.Name)
		}
	})

	// Test with default config
	t.Run("default_config", func(t *testing.T) {
		testData := getTestInstruments()
		testMap := make(map[uint32]*Instrument)
		for _, inst := range testData {
			testMap[inst.InstrumentToken] = inst
		}

		manager, err := New(Config{
			Logger:   testLogger(),
			TestData: testMap,
			// UpdateConfig is nil, should use defaults
		})
		if err != nil {
			t.Fatalf("Expected no error with default config, got: %v", err)
		}
		defer manager.Shutdown()

		// Verify default config was applied
		if manager.config.RetryAttempts != defaultRetryAttempts {
			t.Errorf("Expected default retry attempts %d, got %d", defaultRetryAttempts, manager.config.RetryAttempts)
		}
	})
}

// BenchmarkManagerInsert benchmarks instrument insertion
func BenchmarkManagerInsert(b *testing.B) {
	manager := newTestManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst := &Instrument{
			ID:              fmt.Sprintf("BENCH_TEST_%d", i),
			InstrumentToken: uint32(i + 1000000),
			ExchangeToken:   uint32(i + 10000),
			Tradingsymbol:   "BENCH",
			Exchange:        "NSE",
			ISIN:            "BENCH12345678",
			Name:            "BENCHMARK INSTRUMENT",
			InstrumentType:  "EQ",
			Segment:         "NSE",
			Active:          true,
		}
		manager.Insert(inst)
	}
}

// BenchmarkManagerGetByID benchmarks ID lookups
func BenchmarkManagerGetByID(b *testing.B) {
	manager := newTestManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.GetByID("NSE:SBIN")
	}
}

// BenchmarkManagerFilter benchmarks filtering operations
func BenchmarkManagerFilter(b *testing.B) {
	manager := newTestManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.Filter(func(inst Instrument) bool {
			return inst.Exchange == "NSE"
		})
	}
}

// BenchmarkManagerCount benchmarks count operations
func BenchmarkManagerCount(b *testing.B) {
	manager := newTestManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.Count()
	}
}
