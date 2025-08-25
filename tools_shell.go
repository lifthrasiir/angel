package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// runningProcessInfo stores details of a running command and its completion channel.
type runningProcessInfo struct {
	Cmd  *exec.Cmd
	Done chan struct{} // Closed when the command goroutine finishes
}

// In-memory map to store details of currently running commands.
// This is separate from the DB and is lost on Angel restart.
// DB is the source of truth for persistence.
var runningProcesses = make(map[string]*runningProcessInfo) // Changed type here
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

	ticker := time.NewTicker(1 * time.Second)
	go func() {
		for range ticker.C {
			manageRunningCommands(db)
		}
	}()
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

// manageRunningCommands periodically checks and manages all running shell commands.
func manageRunningCommands(db *sql.DB) {
	cmds, err := GetAllRunningShellCommands(db)
	if err != nil {
		log.Printf("Error fetching running shell commands from DB: %v", err)
		return
	}

	for _, cmdDB := range cmds {
		runningProcessesMutex.Lock()
		info, found := runningProcesses[cmdDB.ID]
		runningProcessesMutex.Unlock()

		// Case 1: Command is running in DB but not in memory (Angel restarted or external termination)
		if !found {
			log.Printf("Command ID %s (%s) found in DB as 'running' but not in memory. Marking as 'failed'.", cmdDB.ID, cmdDB.Command)
			cmdDB.Status = "failed"
			cmdDB.EndTime = sql.NullInt64{Int64: time.Now().Unix(), Valid: true}
			cmdDB.ErrorMessage = sql.NullString{String: "Command process was not found or terminated unexpectedly.", Valid: true}
			if err := UpdateShellCommand(db, cmdDB); err != nil {
				log.Printf("Error updating DB for missing command %s: %v", cmdDB.ID, err)
			}
			continue
		}

		// Case 2: Command is running, check if it's past its current next_poll_delay
		// This check is primarily for updating the DB if the command finishes in the background
		// without an explicit poll from the agent. The exponential backoff for polling
		// is now handled within PollShellCommandTool.
		if info.Cmd.ProcessState != nil && info.Cmd.ProcessState.Exited() { // Changed to info.Cmd
			// Command has finished, update DB from ProcessState
			updateCmdStateFromProcessState(db, cmdDB.ID, info.Cmd) // Changed to info.Cmd
		}
	}
}

// updateCmdStateFromProcessState is called when a command has exited.
// It retrieves final output and updates the DB.
func updateCmdStateFromProcessState(db DbOrTx, cmdID string, execCmd *exec.Cmd) {
	cmdDB, err := GetShellCommandByID(db, cmdID)
	if err != nil {
		log.Printf("Error getting command %s from DB for final update: %v", cmdID, err)
		return
	}

	// Update the full stdout/stderr content
	cmdDB.Stdout = execCmd.Stdout.(*bytes.Buffer).Bytes()
	cmdDB.Stderr = execCmd.Stderr.(*bytes.Buffer).Bytes()

	// Set final offsets to full length
	cmdDB.StdoutOffset = int64(len(cmdDB.Stdout))
	cmdDB.StderrOffset = int64(len(cmdDB.Stderr))

	if execCmd.ProcessState != nil {
		cmdDB.ExitCode = sql.NullInt64{Int64: int64(execCmd.ProcessState.ExitCode()), Valid: true}
		if execCmd.ProcessState.Success() {
			cmdDB.Status = "completed"
		} else {
			cmdDB.Status = "failed"
			cmdDB.ErrorMessage = sql.NullString{String: execCmd.ProcessState.String(), Valid: true}
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
	delete(runningProcesses, cmdID) // Remove from in-memory map
	delete(cmdIDToBranchID, cmdID)  // Remove mapping
	runningProcessesMutex.Unlock()
	log.Printf("Command %s updated to final status: %s", cmdID, cmdDB.Status)
}

// RunShellCommandTool handles the run_shell_command tool call.
func RunShellCommandTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (map[string]interface{}, error) {
	db, err := getDbFromContext(ctx) // Get DB from context
	if err != nil {
		return nil, fmt.Errorf("failed to get DB from context: %w", err)
	}

	if err := EnsureKnownKeys("run_shell_command", args, "command"); err != nil {
		return nil, err
	}
	commandStr, ok := args["command"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid command argument for run_shell_command")
	}

	if !params.ConfirmationReceived {
		// If not confirmed, return a confirmation request
		return nil, &PendingConfirmation{
			Data: map[string]interface{}{
				"tool":    "run_shell_command",
				"command": commandStr,
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

	cmdCtx, cancel := context.WithCancel(context.Background())
	var execCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		execCmd = exec.CommandContext(cmdCtx, "cmd.exe", "/C", commandStr) // For Windows
	} else {
		execCmd = exec.CommandContext(cmdCtx, "bash", "-c", commandStr) // For Unix/Linux
	}

	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	execCmd.Stdout = stdoutBuf
	execCmd.Stderr = stderrBuf

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
		cancel() // Cancel the command context on DB error
		return nil, fmt.Errorf("failed to insert shell command into DB: %w", err)
	}

	// Use a channel to signal when the command goroutine finishes
	doneChan := make(chan struct{})

	// Store execCmd and its cancel function in memory
	runningProcessesMutex.Lock()
	runningProcesses[cmdID] = &runningProcessInfo{Cmd: execCmd, Done: doneChan} // Store doneChan
	runningProcessesMutex.Unlock()

	go func() {
		defer close(doneChan) // Signal completion by closing the channel
		defer cancel()        // Ensure context is cancelled when goroutine exits
		_ = execCmd.Run()     // Run the command; errors will be captured by ProcessState
		log.Printf("Command '%s' (ID: %s) goroutine finished.", commandStr, cmdID)
	}()

	// Check if the command finishes very quickly (within InitialPollDelayInSeconds)
	select {
	case <-doneChan:
		// Command finished within the initial delay
		log.Printf("Command '%s' (ID: %s) finished immediately. Updating DB.", commandStr, cmdID)
		// Update DB with final status
		updateCmdStateFromProcessState(db, cmdID, execCmd)
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
		return result, nil
	case <-time.After(InitialPollDelayInSeconds * time.Second):
		// Command is still running after the initial delay, proceed with normal tracking
		return map[string]interface{}{
			"command_id":      cmdID,
			"status":          "running",
			"elapsed_seconds": InitialPollDelayInSeconds, // After initial wait
		}, nil
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
func PollShellCommandTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (map[string]interface{}, error) {
	db, err := getDbFromContext(ctx) // Get DB from context
	if err != nil {
		return nil, fmt.Errorf("failed to get DB from context: %w", err)
	}

	if err := EnsureKnownKeys("poll_shell_command", args, "command_id"); err != nil {
		return nil, err
	}
	cmdID, ok := args["command_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid command_id argument for poll_shell_command")
	}

	cmdDB, err := GetShellCommandByID(db, cmdID)
	if err != nil {
		return nil, fmt.Errorf("command with ID %s not found in DB: %w", cmdID, err)
	}

	// If the command is still running, wait for the NextPollDelay
	if cmdDB.Status == "running" {
		delay := time.Duration(cmdDB.NextPollDelay) * time.Second
		log.Printf("Polling command %s. Waiting for %v before returning status.", cmdID, delay)

		// Get the done channel for the command
		runningProcessesMutex.Lock()
		info, found := runningProcesses[cmdID]
		runningProcessesMutex.Unlock()

		if found {
			select {
			case <-info.Done:
				// Command finished while waiting, exit early
				log.Printf("Command %s finished during poll delay. Exiting early.", cmdID)

				// Get the execCmd from runningProcesses map
				runningProcessesMutex.Lock()
				currentInfo, foundInMap := runningProcesses[cmdID]
				runningProcessesMutex.Unlock()

				if foundInMap && currentInfo.Cmd.ProcessState != nil && currentInfo.Cmd.ProcessState.Exited() {
					// Command has truly exited, update DB immediately
					updateCmdStateFromProcessState(db, cmdID, currentInfo.Cmd)
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
				log.Printf("Command %s still running after poll delay.", cmdID)
			}
		} else {
			// Command not found in memory, it must have finished and been cleaned up by manageRunningCommands
			// No need to wait, proceed to check status
			log.Printf("Command %s not found in memory during poll delay. Assuming finished.", cmdID)
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
	var execCmd *exec.Cmd
	if info, foundInMap := runningProcesses[cmdID]; foundInMap {
		execCmd = info.Cmd
	}
	runningProcessesMutex.Unlock()

	var newStdout, newStderr []byte
	var currentStdoutLen, currentStderrLen int64

	if execCmd != nil { // Command still in memory
		currentStdoutLen = int64(execCmd.Stdout.(*bytes.Buffer).Len())
		currentStderrLen = int64(execCmd.Stderr.(*bytes.Buffer).Len())

		if currentStdoutLen > cmdDB.StdoutOffset {
			newStdout = execCmd.Stdout.(*bytes.Buffer).Bytes()[cmdDB.StdoutOffset:currentStdoutLen]
		}
		if currentStderrLen > cmdDB.StderrOffset {
			newStderr = execCmd.Stderr.(*bytes.Buffer).Bytes()[cmdDB.StderrOffset:currentStderrLen]
		}
	} else { // Command not in memory, meaning it has finished and DB has full results
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
	return result, nil
}

// KillShellCommandTool handles the kill_shell_command tool call.
func KillShellCommandTool(ctx context.Context, args map[string]interface{}, params ToolHandlerParams) (map[string]interface{}, error) {
	db, err := getDbFromContext(ctx) // Get DB from context
	if err != nil {
		return nil, fmt.Errorf("failed to get DB from context: %w", err)
	}

	if err := EnsureKnownKeys("kill_shell_command", args, "command_id"); err != nil {
		return nil, err
	}
	cmdID, ok := args["command_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid command_id argument for kill_shell_command")
	}

	cmdDB, err := GetShellCommandByID(db, cmdID)
	if err != nil {
		return nil, fmt.Errorf("command with ID %s not found in DB: %w", cmdID, err)
	}

	if cmdDB.Status != "running" {
		return map[string]interface{}{
			"command_id": cmdID,
			"status":     cmdDB.Status,
			"message":    fmt.Sprintf("Command %s is not running (status: %s). No action taken.", cmdID, cmdDB.Status),
		}, nil
	}

	runningProcessesMutex.Lock()
	var execCmd *exec.Cmd
	if info, foundInMap := runningProcesses[cmdID]; foundInMap {
		execCmd = info.Cmd
	}
	runningProcessesMutex.Unlock()

	if execCmd == nil {
		// Command was running in DB but not in memory (already failed/killed externally)
		cmdDB.Status = "failed"
		cmdDB.EndTime = sql.NullInt64{Int64: time.Now().Unix(), Valid: true}
		cmdDB.ErrorMessage = sql.NullString{String: "Command process not found in memory. It might have already terminated.", Valid: true}
		if err := UpdateShellCommand(db, *cmdDB); err != nil {
			log.Printf("Error updating DB for missing command on kill attempt %s: %v", cmdID, err)
		}
		return map[string]interface{}{
			"command_id": cmdID,
			"status":     "failed",
			"message":    "Command process not found in memory, status updated in DB.",
		}, fmt.Errorf("command %s not found in memory", cmdID)
	}

	// Attempt to kill the process
	if err := execCmd.Process.Kill(); err != nil {
		log.Printf("Failed to kill command %s (PID: %d): %v", cmdID, execCmd.Process.Pid, err)
		cmdDB.Status = "failed_to_kill"
		cmdDB.EndTime = sql.NullInt64{Int64: time.Now().Unix(), Valid: true}
		cmdDB.ErrorMessage = sql.NullString{String: fmt.Sprintf("Failed to kill process: %v", err), Valid: true}
		if err := UpdateShellCommand(db, *cmdDB); err != nil {
			log.Printf("Error updating DB for kill failure %s: %v", cmdID, err)
		}
		return nil, fmt.Errorf("failed to kill command %s: %w", cmdID, err)
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

	// However, we should delete from cmdIDToBranchID here.
	runningProcessesMutex.Lock()
	delete(cmdIDToBranchID, cmdID) // Remove mapping
	runningProcessesMutex.Unlock()

	log.Printf("Command ID %s (PID: %d) killed successfully.", cmdID, execCmd.Process.Pid)

	return map[string]interface{}{
		"command_id": cmdID,
		"status":     "killed",
	}, nil
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
