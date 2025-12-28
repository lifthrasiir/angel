package env

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
)

type envConfigKey struct{}

// EnvConfig holds environment-specific application configuration.
type EnvConfig struct {
	dataDir    string
	sessionDir string
}

// DBPath returns the database file path.
func (c *EnvConfig) DBPath() string { return "angel.db" }

// DataDir returns the base data directory for application data.
func (c *EnvConfig) DataDir() string { return c.dataDir }

// SessionDir returns the session directory path.
func (c *EnvConfig) SessionDir() string { return c.sessionDir }

// NewEnvConfig creates a new EnvConfig with default values.
func NewEnvConfig() *EnvConfig {
	dataDir := "angel-data"
	if _, err := os.Stat("go.mod"); err == nil {
		// go.mod exists in current directory, use _angel-data
		dataDir = "_angel-data"
	}
	sessionDir := filepath.Join(dataDir, "sessions")

	return &EnvConfig{
		dataDir:    dataDir,
		sessionDir: sessionDir,
	}
}

// NewTestEnvConfig creates a new fresh EnvConfig for testing purposes.
func NewTestEnvConfig() *EnvConfig {
	// Use an independent nested directory within the base environment
	config := NewEnvConfig()
	baseDir := config.dataDir
	nonce := rand.Uint32()
	config.dataDir = filepath.Join(baseDir, fmt.Sprintf("test%08X", nonce))
	config.sessionDir = filepath.Join(baseDir, fmt.Sprintf("test%08X-sessions", nonce))
	return config
}

// ContextWithEnvConfig returns a new context with the given EnvConfig.
func ContextWithEnvConfig(ctx context.Context, config *EnvConfig) context.Context {
	return context.WithValue(ctx, envConfigKey{}, config)
}

// EnvConfigFromContext retrieves the EnvConfig instance from the given context.Context.
// Returns an error if no database connection is found in the context.
func EnvConfigFromContext(ctx context.Context) (*EnvConfig, error) {
	config, ok := ctx.Value(envConfigKey{}).(*EnvConfig)
	if !ok || config == nil {
		return nil, fmt.Errorf("env config not found in context")
	}
	return config, nil
}
