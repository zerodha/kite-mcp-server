package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHistoricalDate(t *testing.T) {
	full, err := parseHistoricalDate("2025-07-15 09:15:00", false)
	require.NoError(t, err)
	assert.Equal(t, "2025-07-15 09:15:00", full.Format("2006-01-02 15:04:05"))

	start, err := parseHistoricalDate("2025-07-15", false)
	require.NoError(t, err)
	assert.Equal(t, "2025-07-15 00:00:00", start.Format("2006-01-02 15:04:05"))

	end, err := parseHistoricalDate(" 2025-07-22 ", true)
	require.NoError(t, err)
	assert.Equal(t, "2025-07-22 23:59:59", end.Format("2006-01-02 15:04:05"))

	exactEnd, err := parseHistoricalDate("2025-07-22 15:30:00", true)
	require.NoError(t, err)
	assert.Equal(t, "2025-07-22 15:30:00", exactEnd.Format("2006-01-02 15:04:05"))

	_, err = parseHistoricalDate("15-07-2025", false)
	assert.Error(t, err)

	_, err = parseHistoricalDate("", true)
	assert.Error(t, err)
}
