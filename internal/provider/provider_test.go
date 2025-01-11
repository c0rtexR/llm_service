package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewConfig(t *testing.T) {
	// Test creating a new config
	cfg := NewConfig("test-api-key", "test-model")
	require.NotNil(t, cfg)
	require.Equal(t, "test-api-key", cfg.APIKey)
	require.Equal(t, "test-model", cfg.DefaultModel)
	require.Empty(t, cfg.BaseURL)

	// Test setting custom base URL
	cfg = cfg.WithBaseURL("https://api.test.com")
	require.Equal(t, "https://api.test.com", cfg.BaseURL)
}

func TestConfigChaining(t *testing.T) {
	// Test method chaining
	cfg := NewConfig("test-api-key", "test-model").
		WithBaseURL("https://api.test.com")

	require.NotNil(t, cfg)
	require.Equal(t, "test-api-key", cfg.APIKey)
	require.Equal(t, "test-model", cfg.DefaultModel)
	require.Equal(t, "https://api.test.com", cfg.BaseURL)
}
