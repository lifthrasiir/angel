package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	fsPkg "github.com/lifthrasiir/angel/fs" // Import the fs package
)

// runningProcessInfo stores details of a running command and its completion channel.
type runningProcessInfo struct {
	RunningCommand *fsPkg.RunningCommand
}

// In-memory map to store details of currently running commands.
// This is separate from the DB and is lost on Angel restart.
// DB is the source of truth for persistence.
var runningProcesses = make(map[string]*runningProcessInfo)
var runningProcessesMutex sync.Mutex
var cmdIDToBranchID = make(map[string]string)       // Maps cmdID to BranchID
var branchShellLocks = make(map[string]*sync.Mutex) // BranchID -> Mutex for command concurrency
var branchShellLocksMutex sync.Mutex                // Protects access to branchShellLocks map

const InitialPollDelayInSeconds = 4 // -> 8 -> 16 -> 32 -> 60 -> 60 -> ...
const PollDelayMultiplier = 2
const MaxPollDelayInSeconds = 60

// StartShellCommandManager initializes and starts the background goroutine
// that manages shell command lifecycles, including persistence and backoff.
func StartShellCommandManager(db *sql.DB) {
	log.Println("Starting shell command manager...")

	// Clean up any stale running commands from previous runs
	cleanupStaleCommands(db)
}

// cleanupStaleCommands marks any previously running commands as failed on startup.
// Optimized to use a single SQL UPDATE query.
func cleanupStaleCommands(db *sql.DB) {
	log.Println("Cleaning up stale shell commands from previous branch...")
	result, err := db.Exec(`
		UPDATE shell_commands
		SET status = 'failed_on_startup',
		    end_time = ?,
		    error_message = 'Command failed because Angel restarted.'
		WHERE status = 'running'`,
		time.Now().Unix())
	if err != nil {
		log.Printf("Error updating stale shell commands: %v", err)
		return
	}
	rowsAffected, _ := result.RowsAffected()
	log.Printf("Cleaned up %d stale shell commands.", rowsAffected)
}

// updateCmdStateFromProcessState is called when a command has exited.
// It retrieves final output and updates the DB.
func updateCmdStateFromProcessState(db DbOrTx, cmdID string, rc *fsPkg.RunningCommand) {
	cmdDB, err := GetShellCommandByID(db, cmdID)
	if err != nil {
		log.Printf("Error getting command %s from DB for final update: %v", cmdID, err)
		return
	}

	// Update the full stdout/stderr content using TakeStdout/TakeStderr
	cmdDB.Stdout = append(cmdDB.Stdout, rc.TakeStdout()...)
	cmdDB.Stderr = append(cmdDB.Stderr, rc.TakeStderr()...)

	if rc.Cmd.ProcessState != nil {
		cmdDB.ExitCode = sql.NullInt64{Int64: int64(rc.Cmd.ProcessState.ExitCode()), Valid: true}
		if rc.Cmd.ProcessState.Success() {
			cmdDB.Status = "completed"
		} else {
			cmdDB.Status = "failed"
			cmdDB.ErrorMessage = sql.NullString{String: fmt.Sprintf("Command failed with exit code %d", rc.Cmd.ProcessState.ExitCode()), Valid: true}
		}
	} else {
		// This case should ideally not happen if Exited() is true
		cmdDB.Status = "failed"
		cmdDB.ErrorMessage = sql.NullString{String: "Process state not available after command finished.", Valid: true}
	}

	cmdDB.EndTime = sql.NullInt64{Int64: time.Now().Unix(), Valid: true}
	if err := UpdateShellCommand(db, *cmdDB); err != nil {
		log.Printf("Error updating final state for command %s: %v", cmdID, err)
	}

	runningProcessesMutex.Lock()
	if closeErr := rc.Close(); closeErr != nil {
		log.Printf("Error closing RunningCommand for command %s: %v", cmdID, closeErr)
	}
	delete(runningProcesses, cmdID) // Remove from in-memory map
	delete(cmdIDToBranchID, cmdID)  // Remove mapping
	runningProcessesMutex.Unlock()
	log.Printf("Command %s updated to final status: %s", cmdID, cmdDB.Status)
}

// RunShellCommandTool handles the run_shell_command tool call.
func RunShellCommandTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (ToolHandlerResults, error) {
	db, err := getDbFromContext(ctx) // Get DB from context
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to get DB from context: %w", err)
	}

	sfs, err := getSessionFS(ctx, params.SessionId) // Get SessionFS from tools_fs.go
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to get SessionFS: %w", err)
	}
	defer releaseSessionFS(params.SessionId) // Release SessionFS reference

	if err := EnsureKnownKeys("run_shell_command", args, "command", "directory"); err != nil {
		return ToolHandlerResults{}, err
	}
	commandStr, ok := args["command"].(string)
	if !ok {
		return ToolHandlerResults{}, fmt.Errorf("invalid command argument for run_shell_command")
	}

	workingDir := ""
	if dir, ok := args["directory"].(string); ok {
		workingDir = dir
	}

	if !params.ConfirmationReceived {
		// If not confirmed, return a confirmation request
		return ToolHandlerResults{}, &PendingConfirmation{
			Data: map[string]interface{}{
				"tool":      "run_shell_command",
				"command":   commandStr,
				"directory": workingDir,
			},
		}
	}

	// Acquire branch lock
	branchMu := getBranchShellLock(params.BranchId)
	branchMu.Lock()
	defer branchMu.Unlock() // Ensure lock is released when function exits

	// Generate a unique command ID
	runningProcessesMutex.Lock()
	cmdID := generateID()
	cmdIDToBranchID[cmdID] = params.BranchId
	runningProcessesMutex.Unlock()

	cmdCtx := context.Background()

	rc, err := sfs.Run(cmdCtx, commandStr, workingDir)
	if err != nil {
		log.Printf("RunShellCommandTool: Error preparing command execution for cmdID %s: %v", cmdID, err)
		return ToolHandlerResults{}, fmt.Errorf("failed to prepare command execution: %w", err)
	}
	log.Printf("RunShellCommandTool: Command %s (ID: %s) started.", commandStr, cmdID)

	// Initial DB record - stdout_offset and stderr_offset are 0 for a new command
	cmdDB := ShellCommand{
		ID:            cmdID,
		BranchID:      params.BranchId,
		Command:       commandStr,
		Status:        "running",
		StartTime:     time.Now().Unix(),
		LastPolledAt:  time.Now().Unix(),
		NextPollDelay: InitialPollDelayInSeconds,
		StdoutOffset:  0,
		StderrOffset:  0,
	}

	// Insert into DB immediately to ensure the record exists for updateCmdStateFromProcessState
	if err := InsertShellCommand(db, cmdDB); err != nil {
		rc.Close() // rc.Close() calls rc.Cancel(), which is redundant with deferred cancel() but safe
		return ToolHandlerResults{}, fmt.Errorf("failed to insert shell command into DB: %w", err)
	}

	// Store running command and its completion channel in memory
	runningProcessesMutex.Lock()
	runningProcesses[cmdID] = &runningProcessInfo{RunningCommand: rc}
	runningProcessesMutex.Unlock()

	// Check if the command finishes very quickly (within InitialPollDelayInSeconds)
	select {
	case <-rc.Done():
		log.Printf("RunShellCommandTool: Command '%s' (ID: %s) finished immediately. Updating DB.", commandStr, cmdID)
		// Command finished within the initial delay
		// Update DB with final status
		updateCmdStateFromProcessState(db, cmdID, rc)
		// Return completed status and output
		finalCmd, _ := GetShellCommandByID(db, cmdID) // Fetch updated status (guaranteed to exist now)
		result := map[string]interface{}{
			"command_id":      cmdID,
			"status":          finalCmd.Status,
			"stdout":          string(finalCmd.Stdout),
			"stderr":          string(finalCmd.Stderr),
			"elapsed_seconds": finalCmd.EndTime.Int64 - finalCmd.StartTime,
		}
		if finalCmd.ExitCode.Valid {
			result["exit_code"] = finalCmd.ExitCode.Int64
		}
		if finalCmd.ErrorMessage.Valid {
			result["error_message"] = finalCmd.ErrorMessage.String
		}
		return ToolHandlerResults{Value: result}, nil
	case <-time.After(InitialPollDelayInSeconds * time.Second):
		log.Printf("RunShellCommandTool: Command '%s' (ID: %s) still running after initial delay.", commandStr, cmdID)

		// Take any output accumulated during the initial delay
		initialStdout := rc.TakeStdout()
		initialStderr := rc.TakeStderr()

		// Update cmdDB with initial output and offsets
		cmdDB.Stdout = append(cmdDB.Stdout, initialStdout...)
		cmdDB.Stderr = append(cmdDB.Stderr, initialStderr...)
		cmdDB.StdoutOffset = int64(len(cmdDB.Stdout))
		cmdDB.StderrOffset = int64(len(cmdDB.Stderr))
		if err := UpdateShellCommand(db, cmdDB); err != nil {
			log.Printf("Warning: Failed to update initial stdout/stderr for command %s: %v", cmdID, err)
		}

		result := map[string]interface{}{
			"command_id":      cmdID,
			"status":          "running",
			"elapsed_seconds": InitialPollDelayInSeconds, // After initial wait
		}
		if len(initialStdout) > 0 {
			result["stdout"] = string(initialStdout)
		}
		if len(initialStderr) > 0 {
			result["stderr"] = string(initialStderr)
		}
		return ToolHandlerResults{Value: result}, nil
	}
}

// getBranchShellLock retrieves or creates a mutex for a given branch ID.
func getBranchShellLock(branchID string) *sync.Mutex {
	branchShellLocksMutex.Lock()
	defer branchShellLocksMutex.Unlock()
	mu, found := branchShellLocks[branchID]
	if !found {
		mu = &sync.Mutex{}
		branchShellLocks[branchID] = mu
	}
	return mu
}

// PollShellCommandTool handles the poll_shell_command tool call.
func PollShellCommandTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (ToolHandlerResults, error) {
	db, err := getDbFromContext(ctx) // Get DB from context
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to get DB from context: %w", err)
	}

	if err := EnsureKnownKeys("poll_shell_command", args, "command_id"); err != nil {
		return ToolHandlerResults{}, err
	}
	cmdID, ok := args["command_id"].(string)
	if !ok {
		return ToolHandlerResults{}, fmt.Errorf("invalid command_id argument for poll_shell_command")
	}

	cmdDB, err := GetShellCommandByID(db, cmdID)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("command with ID %s not found in DB: %w", cmdID, err)
	}

	// If the command is still running, wait for the NextPollDelay
	if cmdDB.Status == "running" {
		delay := time.Duration(cmdDB.NextPollDelay) * time.Second

		// Get the done channel for the command
		runningProcessesMutex.Lock()
		info, found := runningProcesses[cmdID]
		runningProcessesMutex.Unlock()

		if found {
			select {
			case <-info.RunningCommand.Done():
				log.Printf("PollShellCommandTool: Command %s finished during poll delay. Exiting early.", cmdID)
				// Command finished while waiting, exit early
				// Get the RunningCommand from runningProcesses map
				runningProcessesMutex.Lock()
				currentInfo, foundInMap := runningProcesses[cmdID]
				runningProcessesMutex.Unlock()

				if foundInMap && currentInfo.RunningCommand.Cmd.ProcessState != nil && currentInfo.RunningCommand.Cmd.ProcessState.Exited() {
					// Command has truly exited, update DB immediately
					updateCmdStateFromProcessState(db, cmdID, currentInfo.RunningCommand)
					// After updating, re-fetch the command from DB to get its final status
					updatedCmdDB, err := GetShellCommandByID(db, cmdID)
					if err == nil {
						cmdDB = updatedCmdDB // Use the updated command DB object
					} else {
						log.Printf("Warning: Failed to re-fetch command %s after early exit update: %v", cmdID, err)
					}
				} else {
					// Command might not have fully exited yet, or was already cleaned up by manageRunningCommands
					// Re-fetch from DB to get the latest status
					updatedCmdDB, err := GetShellCommandByID(db, cmdID)
					if err == nil {
						cmdDB = updatedCmdDB
					} else {
						log.Printf("Warning: Failed to re-fetch command %s after early exit (no immediate update): %v", cmdID, err)
					}
				}
			case <-time.After(delay):

				// Delay completed, command still running
			}
		} else {
			// Command not found in memory, it must have finished and been cleaned up by manageRunningCommands
			// No need to wait, proceed to check status
			log.Printf("PollShellCommandTool: Command %s not found in memory during poll delay. Assuming finished.", cmdID)
			// In this case, cmdDB already holds the final status from the initial GetShellCommandByID
		}

		// After waiting (or early exit), update NextPollDelay for the *next* poll
		newNextPollDelay := min(cmdDB.NextPollDelay*PollDelayMultiplier, MaxPollDelayInSeconds)
		cmdDB.NextPollDelay = newNextPollDelay
	}

	// Update last polled time in DB (agent just polled it)
	cmdDB.LastPolledAt = time.Now().Unix()
	// Do not update NextPollDelay here, it's managed by manageRunningCommands
	if err := UpdateShellCommand(db, *cmdDB); err != nil {
		log.Printf("Warning: Failed to update last_polled_at for command %s: %v", cmdID, err)
	}

	result := map[string]interface{}{
		"command_id": cmdID,
		"status":     cmdDB.Status,
	}

	// Get latest output from in-memory buffer for running commands
	runningProcessesMutex.Lock()
	var rc *fsPkg.RunningCommand
	if info, foundInMap := runningProcesses[cmdID]; foundInMap {
		rc = info.RunningCommand
	}
	runningProcessesMutex.Unlock()

	var newStdout, newStderr []byte
	var currentStdoutLen, currentStderrLen int64

	if rc != nil { // Command still in memory
		newStdout = rc.TakeStdout()
		newStderr = rc.TakeStderr()

		cmdDB.Stdout = append(cmdDB.Stdout, newStdout...)
		cmdDB.Stderr = append(cmdDB.Stderr, newStderr...)

		log.Printf("PollShellCommandTool: rc != nil. Taken stdout len: %d, stderr len: %d", len(newStdout), len(newStderr))

		currentStdoutLen = cmdDB.StdoutOffset + int64(len(newStdout))
		currentStderrLen = cmdDB.StderrOffset + int64(len(newStderr))

		if len(newStdout) > 0 {
			log.Printf("PollShellCommandTool: New stdout content: %s", string(newStdout))
		}
		if len(newStderr) > 0 {
			log.Printf("PollShellCommandTool: New stderr content: %s", string(newStderr))
		}
	} else { // Command not in memory, meaning it has finished and DB has full results
		log.Printf("PollShellCommandTool: Command %s not in memory. Reading final output from DB. StdoutOffset: %d, StderrOffset: %d", cmdID, cmdDB.StdoutOffset, cmdDB.StderrOffset)
		newStdout = cmdDB.Stdout[cmdDB.StdoutOffset:]
		newStderr = cmdDB.Stderr[cmdDB.StderrOffset:]
		currentStdoutLen = int64(len(cmdDB.Stdout)) // Set to full length for final update
		currentStderrLen = int64(len(cmdDB.Stderr)) // Set to full length for final update
	}

	if len(newStdout) > 0 {
		result["stdout"] = string(newStdout)
	}
	if len(newStderr) > 0 {
		result["stderr"] = string(newStderr)
	}

	// Update offsets in DB for next poll
	cmdDB.StdoutOffset = currentStdoutLen
	cmdDB.StderrOffset = currentStderrLen
	if err := UpdateShellCommand(db, *cmdDB); err != nil {
		log.Printf("Warning: Failed to update stdout/stderr offsets for command %s: %v", cmdID, err)
	}

	if cmdDB.Status == "running" {
		result["elapsed_seconds"] = time.Now().Unix() - cmdDB.StartTime
	} else { // If completed, failed, or killed
		if cmdDB.ExitCode.Valid {
			result["exit_code"] = cmdDB.ExitCode.Int64
		}
		if cmdDB.ErrorMessage.Valid {
			result["error_message"] = cmdDB.ErrorMessage.String
		}
		result["elapsed_seconds"] = cmdDB.EndTime.Int64 - cmdDB.StartTime
	}
	return ToolHandlerResults{Value: result}, nil
}

// KillShellCommandTool handles the kill_shell_command tool call.
func KillShellCommandTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (ToolHandlerResults, error) {
	db, err := getDbFromContext(ctx) // Get DB from context
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("failed to get DB from context: %w", err)
	}

	if err := EnsureKnownKeys("kill_shell_command", args, "command_id"); err != nil {
		return ToolHandlerResults{}, err
	}
	cmdID, ok := args["command_id"].(string)
	if !ok {
		return ToolHandlerResults{}, fmt.Errorf("invalid command_id argument for kill_shell_command")
	}

	cmdDB, err := GetShellCommandByID(db, cmdID)
	if err != nil {
		return ToolHandlerResults{}, fmt.Errorf("command with ID %s not found in DB: %w", cmdID, err)
	}

	if cmdDB.Status != "running" {
		return ToolHandlerResults{Value: map[string]interface{}{
			"command_id": cmdID,
			"status":     cmdDB.Status,
			"message":    fmt.Sprintf("Command %s is not running (status: %s). No action taken.", cmdID, cmdDB.Status),
		}}, nil
	}

	runningProcessesMutex.Lock()
	var rc *fsPkg.RunningCommand
	if info, foundInMap := runningProcesses[cmdID]; foundInMap {
		rc = info.RunningCommand
	}
	runningProcessesMutex.Unlock()

	if rc == nil {
		// Command was running in DB but not in memory (already failed/killed externally)
		cmdDB.Status = "failed"
		cmdDB.EndTime = sql.NullInt64{Int64: time.Now().Unix(), Valid: true}
		cmdDB.ErrorMessage = sql.NullString{String: "Command process not found in memory. It might have already terminated.", Valid: true}
		if err := UpdateShellCommand(db, *cmdDB); err != nil {
			log.Printf("Error updating DB for missing command on kill attempt %s: %v", cmdID, err)
		}
		return ToolHandlerResults{Value: map[string]interface{}{
			"command_id": cmdID,
			"status":     "failed",
			"message":    "Command process not found in memory, status updated in DB.",
		}}, fmt.Errorf("command %s not found in memory", cmdID)
	}

	// Attempt to kill the process using RunningCommand.Close()
	if err := rc.Close(); err != nil {
		log.Printf("Failed to kill command %s: %v", cmdID, err)
		cmdDB.Status = "failed_to_kill"
		cmdDB.EndTime = sql.NullInt64{Int64: time.Now().Unix(), Valid: true}
		cmdDB.ErrorMessage = sql.NullString{String: fmt.Sprintf("Failed to kill process: %v", err), Valid: true}
		if err := UpdateShellCommand(db, *cmdDB); err != nil {
			log.Printf("Error updating DB for kill failure %s: %v", cmdID, err)
		}
		return ToolHandlerResults{}, fmt.Errorf("failed to kill command %s: %w", cmdID, err)
	}

	// Update DB status to killed
	cmdDB.Status = "killed"
	cmdDB.EndTime = sql.NullInt64{Int64: time.Now().Unix(), Valid: true}
	cmdDB.ErrorMessage = sql.NullString{String: "Killed by agent.", Valid: true}
	if err := UpdateShellCommand(db, *cmdDB); err != nil {
		log.Printf("Error updating DB for killed command %s: %v", cmdID, err)
	}

	// The goroutine running the command will eventually finish and remove it from runningProcesses
	// after its context is done. We don't delete from map here directly.
	// However, we should delete from cmdIDToBranchID here.
	runningProcessesMutex.Lock()
	delete(cmdIDToBranchID, cmdID) // Remove mapping
	runningProcessesMutex.Unlock()

	log.Printf("Command ID %s killed successfully.", cmdID)

	return ToolHandlerResults{Value: map[string]interface{}{
		"command_id": cmdID,
		"status":     "killed",
	}}, nil
}

var (
	runShellCommandToolDefinition = ToolDefinition{
		Name:        "run_shell_command",
		Description: "Executes a shell command asynchronously. It returns a command ID and the current status of the command. If the command completes immediately, its status will be 'completed' and full output will be included. **CRITICAL: If the command's status is 'running', the agent *must immediately and continuously* monitor its final outcome (status, output, and exit code) by calling `poll_shell_command` with the returned command ID. This polling *must* continue without interruption until the command explicitly reaches a 'completed' or 'failed' state, at which point the agent will notify the user.**",
		Parameters: &Schema{
			Type: TypeObject,
			Properties: map[string]*Schema{
				"command": {
					Type:        TypeString,
					Description: "The shell command to execute.",
				},
				"directory": {
					Type:        TypeString,
					Description: "Optional: The directory to run the command in. Can be absolute or relative to the anonymous root. If omitted, defaults to the anonymous root.",
				},
			},
			Required: []string{"command"},
		},
		Handler: RunShellCommandTool,
	}
	pollShellCommandToolDefinition = ToolDefinition{
		Name:        "poll_shell_command",
		Description: "Polls the status, output (stdout/stderr), and exit code of a previously run shell command using its command ID. This is how the final results of a command initiated by `run_shell_command` are retrieved if it did not complete immediately upon execution.",
		Parameters: &Schema{
			Type: TypeObject,
			Properties: map[string]*Schema{
				"command_id": {
					Type:        TypeString,
					Description: "The ID of the command to poll.",
				},
			},
			Required: []string{"command_id"},
		},
		Handler: PollShellCommandTool,
	}
	killShellCommandToolDefinition = ToolDefinition{
		Name:        "kill_shell_command",
		Description: "Terminates a running shell command using its command ID.",
		Parameters: &Schema{
			Type: TypeObject,
			Properties: map[string]*Schema{
				"command_id": {
					Type:        TypeString,
					Description: "The ID of the command to kill.",
				},
			},
			Required: []string{"command_id"},
		},
		Handler: KillShellCommandTool,
	}
)
