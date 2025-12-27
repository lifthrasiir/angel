package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	. "github.com/lifthrasiir/angel/internal/types"
)

const CSRFKeyName = "csrf_key"

// SaveGlobalPrompts saves a list of global prompts to the database.
// It deletes all existing global prompts and then inserts the new ones.
func SaveGlobalPrompts(db *Database, prompts []PredefinedPrompt) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback on error

	// Validate prompts for uniqueness and non-empty labels
	seenLabels := make(map[string]bool)
	for _, p := range prompts {
		if p.Label == "" {
			return fmt.Errorf("prompt label cannot be empty")
		}
		if seenLabels[p.Label] {
			return fmt.Errorf("duplicate prompt label found: %s", p.Label)
		}
		seenLabels[p.Label] = true
	}

	// Clear existing global prompts
	_, err = tx.Exec("DELETE FROM global_prompts")
	if err != nil {
		return fmt.Errorf("failed to clear existing global prompts: %w", err)
	}

	// Insert new global prompts
	stmt, err := tx.Prepare("INSERT INTO global_prompts (label, value) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement for global prompts: %w", err)
	}
	defer stmt.Close()

	for _, p := range prompts {
		_, err := stmt.Exec(p.Label, p.Value)
		if err != nil {
			return fmt.Errorf("failed to insert global prompt %s: %w", p.Label, err)
		}
	}

	return tx.Commit()
}

// GetGlobalPrompts retrieves all global prompts from the database.
func GetGlobalPrompts(db *Database) ([]PredefinedPrompt, error) {
	rows, err := db.Query("SELECT label, value FROM global_prompts ORDER BY id ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to query global prompts: %w", err)
	}
	defer rows.Close()

	var prompts []PredefinedPrompt
	for rows.Next() {
		var p PredefinedPrompt
		if err := rows.Scan(&p.Label, &p.Value); err != nil {
			return nil, fmt.Errorf("failed to scan global prompt: %w", err)
		}
		prompts = append(prompts, p)
	}

	if prompts == nil {
		return []PredefinedPrompt{}, nil // Return empty slice instead of nil
	}
	return prompts, nil
}

// GetAppConfig retrieves a configuration value from the app_configs table.
func GetAppConfig(db *Database, key string) ([]byte, error) {
	var value []byte
	err := db.QueryRow("SELECT value FROM app_configs WHERE key = ?", key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Key not found, not an error
		}
		return nil, fmt.Errorf("failed to get app config for key %s: %w", key, err)
	}
	return value, nil
}

// SetAppConfig saves a configuration value to the app_configs table.
func SetAppConfig(db *Database, key string, value []byte) error {
	_, err := db.Exec("INSERT OR REPLACE INTO app_configs (key, value) VALUES (?, ?)", key, value)
	if err != nil {
		return fmt.Errorf("failed to set app config for key %s: %w", key, err)
	}
	return nil
}

// SaveMCPServerConfig saves an MCP server configuration to the database.
func SaveMCPServerConfig(db *Database, config MCPServerConfig) error {
	_, err := db.Exec(`
		INSERT OR REPLACE INTO mcp_configs (name, config_json, enabled)
		VALUES (?, ?, ?)
	`, config.Name, string(config.ConfigJSON), config.Enabled)
	if err != nil {
		return fmt.Errorf("failed to save MCP server config: %w", err)
	}
	return nil
}

// GetMCPServerConfigs retrieves all MCP server configurations from the database.
func GetMCPServerConfigs(db *Database) ([]MCPServerConfig, error) {
	rows, err := db.Query("SELECT name, config_json, enabled FROM mcp_configs")
	if err != nil {
		return nil, fmt.Errorf("failed to query MCP server configs: %w", err)
	}
	defer rows.Close()

	var configs []MCPServerConfig
	for rows.Next() {
		var config MCPServerConfig
		var connConfigJSON string
		if err := rows.Scan(&config.Name, &connConfigJSON, &config.Enabled); err != nil {
			return nil, fmt.Errorf("failed to scan MCP server config: %w", err)
		}
		config.ConfigJSON = json.RawMessage(connConfigJSON)
		configs = append(configs, config)
	}
	if configs == nil {
		return []MCPServerConfig{}, nil
	}
	return configs, nil
}

// DeleteMCPServerConfig deletes an MCP server configuration from the database.
func DeleteMCPServerConfig(db *Database, name string) error {
	result, err := db.Exec("DELETE FROM mcp_configs WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("failed to delete MCP server config: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("MCP config with name %s not found", name)
	}
	return nil
}

// getHighestOAuthTokenID returns the highest existing ID in oauth_tokens table
// Returns 0 if table is empty
func getHighestOAuthTokenID(db *Database) (int, error) {
	var highestID int
	err := db.QueryRow("SELECT COALESCE(MAX(id), 0) FROM oauth_tokens").Scan(&highestID)
	if err != nil {
		return 0, fmt.Errorf("failed to get highest OAuth token ID: %w", err)
	}
	return highestID, nil
}

// getNextOAuthTokenID returns the next available ID for new oauth tokens
func getNextOAuthTokenID(db *Database) (int, error) {
	highestID, err := getHighestOAuthTokenID(db)
	if err != nil {
		return 0, err
	}
	return highestID + 1, nil
}

// SaveOAuthToken saves an OAuth token to the database.
// Only token.TokenData, token.UserEmail, token.ProjectID and token.Kind are used.
func SaveOAuthToken(db *Database, token OAuthToken) error {
	// Get the next available ID
	nextID, err := getNextOAuthTokenID(db)
	if err != nil {
		return err
	}

	lastUsedByModelJSON, err := marshalTimeMap(make(map[string]time.Time))
	if err != nil {
		return fmt.Errorf("failed to marshal last_used_by_model: %w", err)
	}

	// Use INSERT OR REPLACE to handle duplicates automatically due to UNIQUE(user_email, kind) constraint
	_, err = db.Exec(
		"INSERT OR REPLACE INTO oauth_tokens (id, token_data, user_email, project_id, kind, last_used_by_model, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
		nextID, token.TokenData, token.UserEmail, token.ProjectID, token.Kind, lastUsedByModelJSON)
	if err != nil {
		return fmt.Errorf("failed to save OAuth token: %w", err)
	}
	log.Printf("Saved OAuth token for user %s with kind %s", token.UserEmail, token.Kind)
	return nil
}

// UpdateOAuthTokenData updates the token data for an existing OAuth token
func UpdateOAuthTokenData(db *Database, tokenID int, tokenJSON string) error {
	_, err := db.Exec(
		"UPDATE oauth_tokens SET token_data = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		tokenJSON, tokenID)
	if err != nil {
		return fmt.Errorf("failed to update OAuth token data: %w", err)
	}
	return nil
}

// DeleteOAuthTokenByEmail deletes a specific OAuth token by user email and kind.
func DeleteOAuthTokenByEmail(db *Database, userEmail string, kind string) error {
	_, err := db.Exec("DELETE FROM oauth_tokens WHERE user_email = ? AND kind = ?", userEmail, kind)
	if err != nil {
		return fmt.Errorf("failed to delete OAuth token for user %s: %w", userEmail, err)
	}
	return nil
}

// DeleteOAuthTokenByID deletes a specific OAuth token by ID.
func DeleteOAuthTokenByID(db *Database, id int) error {
	_, err := db.Exec("DELETE FROM oauth_tokens WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete OAuth token with ID %d: %w", id, err)
	}
	return nil
}

// GetOAuthTokens retrieves all OAuth tokens from the database.
func GetOAuthTokens(db *Database) ([]OAuthToken, error) {
	rows, err := db.Query("SELECT id, token_data, user_email, project_id, kind, last_used_by_model, created_at, updated_at FROM oauth_tokens ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to query OAuth tokens: %w", err)
	}
	defer rows.Close()

	tokens := []OAuthToken{}
	for rows.Next() {
		var token OAuthToken
		var nullUserEmail sql.NullString
		var nullProjectID sql.NullString
		var lastUsedByModelStr sql.NullString

		err := rows.Scan(&token.ID, &token.TokenData, &nullUserEmail, &nullProjectID, &token.Kind, &lastUsedByModelStr, &token.CreatedAt, &token.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan OAuth token: %w", err)
		}

		// Handle NULL fields
		if nullUserEmail.Valid {
			token.UserEmail = nullUserEmail.String
		}
		if nullProjectID.Valid {
			token.ProjectID = nullProjectID.String
		}

		// JSON field deserialization - handle NULL
		var jsonStr string
		if lastUsedByModelStr.Valid {
			jsonStr = lastUsedByModelStr.String
		} else {
			jsonStr = "{}"
		}
		token.LastUsedByModel, err = unmarshalTimeMap(jsonStr)
		if err != nil {
			log.Printf("Failed to unmarshal last_used_by_model for token %d: %v", token.ID, err)
			token.LastUsedByModel = make(map[string]time.Time)
		}

		tokens = append(tokens, token)
	}

	return tokens, nil
}

// GetOAuthTokensWithValidProjectID retrieves only OAuth tokens that have valid (non-empty) project IDs.
func GetOAuthTokensWithValidProjectID(db *Database) ([]OAuthToken, error) {
	rows, err := db.Query("SELECT id, token_data, user_email, project_id, kind, last_used_by_model, created_at, updated_at FROM oauth_tokens WHERE project_id IS NOT NULL AND project_id != '' ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to query OAuth tokens with valid project IDs: %w", err)
	}
	defer rows.Close()

	tokens := []OAuthToken{}
	for rows.Next() {
		var token OAuthToken
		var nullUserEmail sql.NullString
		var nullProjectID sql.NullString
		var lastUsedByModelStr sql.NullString

		err := rows.Scan(&token.ID, &token.TokenData, &nullUserEmail, &nullProjectID, &token.Kind, &lastUsedByModelStr, &token.CreatedAt, &token.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan OAuth token: %w", err)
		}

		// Handle NULL fields
		if nullUserEmail.Valid {
			token.UserEmail = nullUserEmail.String
		}
		if nullProjectID.Valid {
			token.ProjectID = nullProjectID.String
		}

		// JSON field deserialization - handle NULL
		var jsonStr string
		if lastUsedByModelStr.Valid {
			jsonStr = lastUsedByModelStr.String
		} else {
			jsonStr = "{}"
		}
		token.LastUsedByModel, err = unmarshalTimeMap(jsonStr)
		if err != nil {
			log.Printf("Failed to unmarshal last_used_by_model for token %d: %v", token.ID, err)
			token.LastUsedByModel = make(map[string]time.Time)
		}

		tokens = append(tokens, token)
	}

	return tokens, nil
}

// UpdateOAuthTokenModelLastUsed updates the last used time for a specific model in the OAuth token.
func UpdateOAuthTokenModelLastUsed(db *Database, id int, modelName string) error {
	token, err := GetOAuthToken(db, id)
	if err != nil {
		return err
	}

	// Update the last used time for the model
	if token.LastUsedByModel == nil {
		token.LastUsedByModel = make(map[string]time.Time)
	}
	token.LastUsedByModel[modelName] = time.Now()

	lastUsedByModelJSON, err := marshalTimeMap(token.LastUsedByModel)
	if err != nil {
		return fmt.Errorf("failed to marshal last_used_by_model: %w", err)
	}

	_, err = db.Exec("UPDATE oauth_tokens SET last_used_by_model = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", lastUsedByModelJSON, id)
	if err != nil {
		return fmt.Errorf("failed to update OAuth token last used time: %w", err)
	}

	return nil
}

// HandleOAuthRateLimit handles rate limiting for OAuth tokens by updating the model's last used time with a future timestamp.
func HandleOAuthRateLimit(db *Database, id int, modelName string, retryAfter time.Duration) error {
	// Current time + retryAfter + buffer (30 seconds)
	futureTime := time.Now().Add(retryAfter).Add(30 * time.Second)

	// Get the current token
	token, err := GetOAuthToken(db, id)
	if err != nil {
		return err
	}

	// Initialize and update the map
	if token.LastUsedByModel == nil {
		token.LastUsedByModel = make(map[string]time.Time)
	}
	token.LastUsedByModel[modelName] = futureTime

	lastUsedByModelJSON, err := marshalTimeMap(token.LastUsedByModel)
	if err != nil {
		return fmt.Errorf("failed to marshal last_used_by_model: %w", err)
	}

	_, err = db.Exec("UPDATE oauth_tokens SET last_used_by_model = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", lastUsedByModelJSON, id)
	if err != nil {
		return fmt.Errorf("failed to update OAuth token last used time: %w", err)
	}

	return nil
}

// GetOAuthToken retrieves a specific OAuth token by ID.
func GetOAuthToken(db *Database, id int) (*OAuthToken, error) {
	var token OAuthToken
	var nullUserEmail sql.NullString
	var nullProjectID sql.NullString
	var lastUsedByModelStr sql.NullString

	err := db.QueryRow("SELECT id, token_data, user_email, project_id, kind, last_used_by_model, created_at, updated_at FROM oauth_tokens WHERE id = ?", id).
		Scan(&token.ID, &token.TokenData, &nullUserEmail, &nullProjectID, &token.Kind, &lastUsedByModelStr, &token.CreatedAt, &token.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get OAuth token: %w", err)
	}

	// Handle NULL fields
	if nullUserEmail.Valid {
		token.UserEmail = nullUserEmail.String
	}
	if nullProjectID.Valid {
		token.ProjectID = nullProjectID.String
	}

	// JSON field deserialization - handle NULL
	var jsonStr string
	if lastUsedByModelStr.Valid {
		jsonStr = lastUsedByModelStr.String
	} else {
		jsonStr = "{}"
	}
	token.LastUsedByModel, err = unmarshalTimeMap(jsonStr)
	if err != nil {
		log.Printf("Failed to unmarshal last_used_by_model for token %d: %v", token.ID, err)
		token.LastUsedByModel = make(map[string]time.Time)
	}

	return &token, nil
}

// SaveOpenAIConfig saves an OpenAI configuration to the database.
func SaveOpenAIConfig(db *Database, config OpenAIConfig) error {
	_, err := db.Exec(`
		INSERT OR REPLACE INTO openai_configs (id, name, endpoint, api_key, enabled, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, config.ID, config.Name, config.Endpoint, config.APIKey, config.Enabled)
	if err != nil {
		return fmt.Errorf("failed to save OpenAI config: %w", err)
	}
	return nil
}

// GetOpenAIConfigs retrieves all OpenAI configurations from the database.
func GetOpenAIConfigs(db *Database) ([]OpenAIConfig, error) {
	rows, err := db.Query("SELECT id, name, endpoint, api_key, enabled, created_at, updated_at FROM openai_configs ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to query OpenAI configs: %w", err)
	}
	defer rows.Close()

	var configs []OpenAIConfig
	for rows.Next() {
		var config OpenAIConfig
		err := rows.Scan(&config.ID, &config.Name, &config.Endpoint, &config.APIKey, &config.Enabled, &config.CreatedAt, &config.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan OpenAI config: %w", err)
		}
		configs = append(configs, config)
	}
	if configs == nil {
		return []OpenAIConfig{}, nil
	}
	return configs, nil
}

// GetOpenAIConfig retrieves a single OpenAI configuration by its ID.
func GetOpenAIConfig(db *Database, id string) (*OpenAIConfig, error) {
	var config OpenAIConfig
	err := db.QueryRow("SELECT id, name, endpoint, api_key, enabled, created_at, updated_at FROM openai_configs WHERE id = ?", id).
		Scan(&config.ID, &config.Name, &config.Endpoint, &config.APIKey, &config.Enabled, &config.CreatedAt, &config.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("OpenAI config with id %s not found", id)
		}
		return nil, fmt.Errorf("failed to get OpenAI config: %w", err)
	}
	return &config, nil
}

// DeleteOpenAIConfig deletes an OpenAI configuration from the database.
func DeleteOpenAIConfig(db *Database, id string) error {
	result, err := db.Exec("DELETE FROM openai_configs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete OpenAI config: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("OpenAI config with id %s not found", id)
	}
	return nil
}

// GetGeminiAPIConfigs retrieves all Gemini API configurations from the database.
func GetGeminiAPIConfigs(db *Database) ([]GeminiAPIConfig, error) {
	rows, err := db.Query("SELECT id, name, api_key, enabled, last_used_by_model, created_at, updated_at FROM gemini_api_configs ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to query Gemini API configs: %w", err)
	}
	defer rows.Close()

	configs := []GeminiAPIConfig{}
	for rows.Next() {
		var config GeminiAPIConfig
		var lastUsedByModelStr sql.NullString

		err := rows.Scan(&config.ID, &config.Name, &config.APIKey, &config.Enabled, &lastUsedByModelStr, &config.CreatedAt, &config.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan Gemini API config: %w", err)
		}

		// JSON field deserialization - handle NULL
		var jsonStr string
		if lastUsedByModelStr.Valid {
			jsonStr = lastUsedByModelStr.String
		} else {
			jsonStr = "{}"
		}
		config.LastUsedByModel, err = unmarshalTimeMap(jsonStr)
		if err != nil {
			log.Printf("Failed to unmarshal last_used_by_model for config %s: %v", config.ID, err)
			config.LastUsedByModel = make(map[string]time.Time)
		}

		configs = append(configs, config)
	}

	return configs, nil
}

// UpdateModelLastUsed updates the last used time for a specific model in the Gemini API configuration.
func UpdateModelLastUsed(db *Database, id, modelName string) error {
	config, err := GetGeminiAPIConfig(db, id)
	if err != nil {
		return err
	}

	// Initialize and update the map
	if config.LastUsedByModel == nil {
		config.LastUsedByModel = make(map[string]time.Time)
	}
	config.LastUsedByModel[modelName] = time.Now()

	// Convert to JSON (time.Time automatically serializes to RFC3339)
	lastUsedJSON, err := marshalTimeMap(config.LastUsedByModel)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		UPDATE gemini_api_configs
		SET last_used_by_model = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, string(lastUsedJSON), id)

	return err
}

// HandleModelRateLimit handles rate limiting for a specific model by updating its last used time with a future timestamp.
func HandleModelRateLimit(db *Database, id, modelName string, retryAfter time.Duration) error {
	// Current time + retryAfter + buffer (30 seconds)
	futureTime := time.Now().Add(retryAfter).Add(30 * time.Second)

	// Get the current configuration
	config, err := GetGeminiAPIConfig(db, id)
	if err != nil {
		return err
	}

	// Initialize and update the map
	if config.LastUsedByModel == nil {
		config.LastUsedByModel = make(map[string]time.Time)
	}
	config.LastUsedByModel[modelName] = futureTime

	// Convert to JSON
	lastUsedJSON, err := marshalTimeMap(config.LastUsedByModel)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		UPDATE gemini_api_configs
		SET last_used_by_model = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, string(lastUsedJSON), id)

	return err
}

// SaveGeminiAPIConfig saves a Gemini API configuration to the database.
func SaveGeminiAPIConfig(db *Database, config GeminiAPIConfig) error {
	_, err := db.Exec(`
		INSERT OR REPLACE INTO gemini_api_configs (id, name, api_key, enabled, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, config.ID, config.Name, config.APIKey, config.Enabled)
	if err != nil {
		return fmt.Errorf("failed to save Gemini API config: %w", err)
	}
	return nil
}

// GetGeminiAPIConfig retrieves a single Gemini API configuration by its ID.
func GetGeminiAPIConfig(db *Database, id string) (*GeminiAPIConfig, error) {
	var config GeminiAPIConfig
	var lastUsedByModelStr sql.NullString
	err := db.QueryRow("SELECT id, name, api_key, enabled, last_used_by_model, created_at, updated_at FROM gemini_api_configs WHERE id = ?", id).
		Scan(&config.ID, &config.Name, &config.APIKey, &config.Enabled, &lastUsedByModelStr, &config.CreatedAt, &config.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			//lint:ignore ST1005 Gemini is a proper noun
			return nil, fmt.Errorf("Gemini API config with id %s not found", id)
		}
		return nil, fmt.Errorf("failed to get Gemini API config: %w", err)
	}

	// JSON field deserialization - handle NULL
	var jsonStr string
	if lastUsedByModelStr.Valid {
		jsonStr = lastUsedByModelStr.String
	} else {
		jsonStr = "{}"
	}
	config.LastUsedByModel, err = unmarshalTimeMap(jsonStr)
	if err != nil {
		log.Printf("Failed to unmarshal last_used_by_model for config %s: %v", config.ID, err)
		config.LastUsedByModel = make(map[string]time.Time)
	}

	return &config, nil
}

// DeleteGeminiAPIConfig deletes a Gemini API configuration from the database.
func DeleteGeminiAPIConfig(db *Database, id string) error {
	result, err := db.Exec("DELETE FROM gemini_api_configs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete Gemini API config: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		//lint:ignore ST1005 Gemini is a proper noun
		return fmt.Errorf("Gemini API config with id %s not found", id)
	}
	return nil
}

// marshalTimeMap converts a map of time.Time to JSON (uses default serialization)
func marshalTimeMap(m map[string]time.Time) ([]byte, error) {
	return json.Marshal(m)
}

// unmarshalTimeMap converts JSON to a map of time.Time
func unmarshalTimeMap(data string) (map[string]time.Time, error) {
	var result map[string]time.Time
	if data == "" || data == "null" {
		return make(map[string]time.Time), nil
	}

	err := json.Unmarshal([]byte(data), &result)
	if err != nil {
		log.Printf("Failed to unmarshal time map: %v, data: %s", err, data)
		return make(map[string]time.Time), nil
	}
	return result, nil
}
