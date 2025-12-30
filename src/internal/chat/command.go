package chat

import (
	"context"
	"database/sql"
	"fmt"

	. "github.com/lifthrasiir/angel/gemini"
	"github.com/lifthrasiir/angel/internal/database"
	. "github.com/lifthrasiir/angel/internal/types"
)

// ExecuteClearCommand executes clear or clearblobs command by creating a command message
func ExecuteClearCommand(ctx context.Context, db database.SessionDbOrTx, command string) (int, error) {
	// Get the primary branch for the session
	databaseDB, ok := db.(*database.SessionDatabase)
	if !ok {
		return 0, fmt.Errorf("expected *database.SessionDatabase, got %T", db)
	}
	session, err := database.GetSession(databaseDB)
	if err != nil {
		return 0, fmt.Errorf("failed to get session: %w", err)
	}
	primaryBranchID := session.PrimaryBranchID

	// Create a command message
	commandMsg := Message{
		LocalSessionID: db.LocalSessionId(),
		BranchID:       primaryBranchID,
		Text:           command,
		Type:           TypeCommand,
		Model:          DefaultGeminiModel,
	}

	// Get the last message in the branch to link properly
	lastMessageID, _, _, err := database.GetLastMessageInBranch(db, primaryBranchID)
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("failed to get last message in branch: %w", err)
	}

	if lastMessageID != 0 {
		commandMsg.ParentMessageID = &lastMessageID
	}

	// Add the command message to the database
	commandMessageID, err := database.AddMessageToSession(ctx, db, commandMsg)
	if err != nil {
		return 0, fmt.Errorf("failed to add command message: %w", err)
	}

	// Update the previous message's chosen_next_id to point to the command
	if lastMessageID != 0 {
		if err := database.UpdateMessageChosenNextID(db, lastMessageID, &commandMessageID); err != nil {
			return 0, fmt.Errorf("failed to update chosen_next_id for previous message: %w", err)
		}
	} else {
		// This is the first message, update session's chosen_first_id
		if err := database.UpdateSessionChosenFirstID(databaseDB, &commandMessageID); err != nil {
			// Non-fatal error, log but continue
			fmt.Printf("Warning: Failed to update chosen_first_id for session %s: %v\n", db.SessionId(), err)
		}
	}

	return commandMessageID, nil
}
