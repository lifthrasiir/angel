package database

import (
	"database/sql"
	"fmt"
	"strings"
)

// SearchResult represents a single search result
type SearchResult struct {
	MessageID   int    `json:"message_id"`
	SessionID   string `json:"session_id"`
	Excerpt     string `json:"excerpt"`
	Type        string `json:"type"`
	CreatedAt   string `json:"created_at"`
	SessionName string `json:"session_name"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// SearchMessages searches for messages matching the query using FTS5 tables
func SearchMessages(db *sql.DB, query string, maxID int, limit int, workspaceID string) ([]SearchResult, bool, error) {
	// Validate query
	if strings.TrimSpace(query) == "" {
		return nil, false, fmt.Errorf("search query cannot be empty")
	}

	// Set default limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100 // Cap at 100 for performance
	}

	// Build search query with snippet directly from FTS tables
	// Use source_order to prefer stems over trigrams when both match the same message
	searchSubQueryFormat := `
		SELECT
			rowid as id,
			session_id,
			workspace_id,
			-- Convert SI/SO back to HTML tags, then escape for safe display
			replace(
				replace(
					replace(
						snippet(%s, 0, '<mark>', '</mark>', '...', 64),
						'&', '&amp;'
					),
					'\x0e', '&lt;'
				),
				'\x0f', '&gt;'
			) as excerpt,
			%d as source_order -- Add source order to identify which table this came from (stems=0, trigrams=1)
		FROM %[1]s WHERE %[1]s MATCH ?
	`
	args := []interface{}{query, query}
	if workspaceID != "" {
		searchSubQueryFormat += " AND workspace_id = ?"
		args = []interface{}{query, workspaceID, query, workspaceID}
	}

	// Combine results from both tables, preferring stems (order 0) over trigrams (order 1) for duplicates
	searchSubQuery := `
		SELECT id, session_id, workspace_id, excerpt
		FROM (
			SELECT * FROM (` + fmt.Sprintf(searchSubQueryFormat, "message_stems", 0) + `)
			UNION ALL
			SELECT * FROM (` + fmt.Sprintf(searchSubQueryFormat, "message_trigrams", 1) + `)
		)
		GROUP BY id
		ORDER BY MIN(source_order)
	`

	// Build final query joining with messages and sessions
	baseQuery := `
		SELECT DISTINCT
			m.id,
			m.session_id,
			fts.excerpt,
			m.type,
			m.created_at,
			COALESCE(s.name, '') as session_name,
			fts.workspace_id
		FROM messages m
		JOIN sessions s ON m.session_id = s.id
		JOIN (` + searchSubQuery + `) fts ON m.id = fts.id
	`

	// Add max_id filter for pagination (get messages older than max_id)
	if maxID > 0 {
		baseQuery += " WHERE m.id < ?"
		args = append(args, maxID)
	}

	// Order by message ID (descending for newest first) and limit
	baseQuery += " ORDER BY m.id DESC LIMIT ?"
	args = append(args, limit+1) // Request one more to check if there are more results

	rows, err := db.Query(baseQuery, args...)
	if err != nil {
		return nil, false, fmt.Errorf("failed to search messages: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var result SearchResult
		err := rows.Scan(
			&result.MessageID,
			&result.SessionID,
			&result.Excerpt,
			&result.Type,
			&result.CreatedAt,
			&result.SessionName,
			&result.WorkspaceID,
		)
		if err != nil {
			return nil, false, fmt.Errorf("failed to scan search result: %w", err)
		}
		results = append(results, result)
	}

	// Check if there are more results
	hasMore := len(results) > limit
	if hasMore {
		// Remove the extra result we used for checking
		results = results[:limit]
	}

	return results, hasMore, nil
}
