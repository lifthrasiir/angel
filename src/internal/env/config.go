package env

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type envConfigKey struct{}

// EnvConfig holds environment-specific application configuration.
type EnvConfig struct {
	dataDir     string
	sessionDir  string
	useMemoryDB bool // If true, use in-memory databases for testing
}

// DBPath returns the database file path.
func (c *EnvConfig) DBPath() string { return "angel.db" }

// DataDir returns the base data directory for application data.
func (c *EnvConfig) DataDir() string { return c.dataDir }

// SessionDir returns the session directory path.
func (c *EnvConfig) SessionDir() string { return c.sessionDir }

// UseMemoryDB returns true if in-memory databases should be used (for testing).
func (c *EnvConfig) UseMemoryDB() bool { return c.useMemoryDB }

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
// If useMemoryDB is true, uses in-memory databases for better performance.
// If false, uses real files for testing file system operations.
func NewTestEnvConfig(useMemoryDB bool) *EnvConfig {
	config := NewEnvConfig()
	config.useMemoryDB = useMemoryDB
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
