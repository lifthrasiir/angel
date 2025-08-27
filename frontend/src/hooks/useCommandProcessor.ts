import { useAtomValue, useSetAtom } from 'jotai';
import { apiFetch } from '../api/apiClient';
import {
  statusMessageAtom,
  workspaceIdAtom,
  sessionsAtom,
  isPickingDirectoryAtom,
  temporaryEnvChangeMessageAtom,
  primaryBranchIdAtom,
  pendingRootsAtom, // Add this import
} from '../atoms/chatAtoms';
import type { ChatMessage, RootsChanged, EnvChanged } from '../types/chat'; // Added EnvChanged
import { fetchSessions } from '../utils/sessionManager';
import { callNativeDirectoryPicker, ResultType, PickDirectoryAPIResponse } from '../utils/dialogHelpers';

export const useCommandProcessor = (sessionId: string | null) => {
  const setStatusMessage = useSetAtom(statusMessageAtom);
  const workspaceId = useAtomValue(workspaceIdAtom);
  const setSessions = useSetAtom(sessionsAtom);
  const setIsPickingDirectory = useSetAtom(isPickingDirectoryAtom);
  const setTemporaryEnvChangeMessage = useSetAtom(temporaryEnvChangeMessageAtom);
  const primaryBranchId = useAtomValue(primaryBranchIdAtom);
  const setPendingRoots = useSetAtom(pendingRootsAtom); // Add this line
  const currentPendingRoots = useAtomValue(pendingRootsAtom); // Get current value for calculations

  const runCompress = async () => {
    setStatusMessage('Compressing chat history...');
    try {
      const response = await apiFetch(`/api/chat/${sessionId}/compress`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
      });
      if (!response.ok) {
        const errorData = await response.json();
        throw new Error(errorData.message || 'Failed to compress chat history');
      }
      const result = await response.json();
      setStatusMessage(
        `Compression successful! Original tokens: ${result.originalTokenCount}, New tokens: ${result.newTokenCount}`,
      );

      // Refresh the session list
      if (workspaceId) {
        try {
          const workspaceWithSessions = await fetchSessions(workspaceId);
          setSessions(workspaceWithSessions.sessions);
        } catch (refreshError) {
          console.error('Failed to refresh sessions after compression:', refreshError);
        }
      }
    } catch (error: any) {
      setStatusMessage(`Compression failed: ${error.message}`);
      console.error('Compression failed:', error);
    }
  };

  // updateRoots function - MODIFIED
  const updateRoots = async (command: string, rootsToProcess: string[]): Promise<RootsChanged | undefined> => {
    setStatusMessage(`Updating exposed directories...`);

    // Determine if we are in a new session context (sessionId is null)
    const isNewSessionContext = !sessionId;

    let targetRoots: string[] = [];
    if (isNewSessionContext) {
      // For new session context, update pendingRootsAtom
      let newPendingRoots: string[] = [...currentPendingRoots];

      if (command === 'unexpose') {
        if (rootsToProcess.length === 0) {
          newPendingRoots = [];
        } else {
          const rootsToRemove = new Set(rootsToProcess);
          newPendingRoots = newPendingRoots.filter((root) => !rootsToRemove.has(root));
        }
      } else {
        // command === 'expose'
        const newRootsSet = new Set([...newPendingRoots, ...rootsToProcess]);
        newPendingRoots = Array.from(newRootsSet);
      }
      setPendingRoots(newPendingRoots);
      targetRoots = newPendingRoots; // Use newPendingRoots for calculation

      // Call backend to get EnvChanged object for new session context
      try {
        const response = await apiFetch(
          `/api/chat/new/envChanged?newRoots=${encodeURIComponent(JSON.stringify(targetRoots))}`,
        );
        if (!response.ok) {
          const errorData = await response.json();
          throw new Error(errorData.message || 'Failed to calculate environment changes');
        }
        const envChanged: EnvChanged = await response.json();

        // Create the actual EnvChanged message
        const newTemporaryEnvChangedMessage: ChatMessage = {
          id: crypto.randomUUID(),
          type: 'env_changed',
          parts: [{ text: JSON.stringify(envChanged) }], // Store the EnvChanged object as JSON string
          sessionId: sessionId || 'new', // Use 'new' for temporary message if no session ID
          branchId: primaryBranchId,
        };
        setTemporaryEnvChangeMessage(newTemporaryEnvChangedMessage);
        return envChanged.roots; // Return rootsChanged part for consistency
      } catch (error: any) {
        setStatusMessage(`Failed to calculate environment changes: ${error.message}`);
        console.error('Failed to calculate environment changes:', error);
        setTemporaryEnvChangeMessage(null);
        return undefined;
      }
    } else {
      // For existing session context, use current session roots
      // Fetch current roots from backend for accurate calculation
      try {
        const sessionResponse = await apiFetch(`/api/chat/${sessionId}`);
        if (!sessionResponse.ok) {
          const errorData = await sessionResponse.json();
          throw new Error(errorData.message || 'Failed to fetch current session roots');
        }
        const sessionData = await sessionResponse.json();
        let currentSessionRoots: string[] = sessionData.roots || [];

        if (command === 'unexpose') {
          if (rootsToProcess.length === 0) {
            targetRoots = [];
          } else {
            const rootsToRemove = new Set(rootsToProcess);
            targetRoots = currentSessionRoots.filter((root) => !rootsToRemove.has(root));
          }
        } else {
          // command === 'expose'
          const newRootsSet = new Set([...currentSessionRoots, ...rootsToProcess]);
          targetRoots = Array.from(newRootsSet);
        }

        const response = await apiFetch(`/api/chat/${sessionId}/roots`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ roots: targetRoots }),
        });

        if (!response.ok) {
          const errorData = await response.json();
          throw new Error(errorData.message || `Failed to ${command} directories`);
        }
        const rootsChanged: RootsChanged = await response.json();
        setStatusMessage(
          rootsChanged.value.length === 0 ? 'No directories are exposed.' : `Directories ${command}d successfully.`,
        );

        // Refresh the session list (to update roots displayed in UI if any)
        if (workspaceId) {
          try {
            const workspaceWithSessions = await fetchSessions(workspaceId);
            setSessions(workspaceWithSessions.sessions);
          } catch (refreshError) {
            console.error('Failed to refresh sessions after roots update:', refreshError);
          }
        }
        return rootsChanged;
      } catch (error: any) {
        setStatusMessage(`${command} failed: ${error.message}`);
        console.error(`${command} failed:`, error);
        return undefined;
      }
    }
  };

  const runExposeOrUnexpose = async (command: string, args: string) => {
    // Create a temporary message to show "Applying changes..."
    const tempMessage: ChatMessage = {
      id: crypto.randomUUID(),
      parts: [{ text: `Applying ${command} changes...` }],
      type: 'system',
      sessionId: sessionId || 'new', // Use 'new' for temporary message if no session ID
      branchId: primaryBranchId,
    };
    setTemporaryEnvChangeMessage(tempMessage);

    let rootsToProcess: string[] = [];
    if (command === 'expose' && !args) {
      // If /expose is called without arguments, trigger native directory picker
      const data: PickDirectoryAPIResponse = await callNativeDirectoryPicker(setIsPickingDirectory, setStatusMessage);

      switch (data.result) {
        case ResultType.Success:
          if (data.selectedPath) {
            rootsToProcess = [data.selectedPath];
          } else {
            setStatusMessage('Error: No path returned from directory picker.');
            setTemporaryEnvChangeMessage(null);
            return;
          }
          break;
        case ResultType.Canceled:
          setStatusMessage('Directory selection canceled.');
          setTemporaryEnvChangeMessage(null);
          return;
        case ResultType.AlreadyOpen:
          setStatusMessage('Another directory picker is already open.');
          setTemporaryEnvChangeMessage(null);
          return;
        case ResultType.Error:
          setStatusMessage(`Error selecting directory: ${data.error || 'Unknown error'}`);
          setTemporaryEnvChangeMessage(null);
          return;
      }
    } else {
      // Existing logic for expose/unexpose with arguments or unexpose without arguments
      if (args) {
        rootsToProcess = args
          .split(',')
          .map((path) => path.trim())
          .filter((path) => path.length > 0);
      } else if (command === 'unexpose') {
        // unexpose without arguments means clear all roots
        rootsToProcess = [];
      }
    }

    // Call the unified updateRoots function
    await updateRoots(command, rootsToProcess);
  };

  const runCommand = async (command: string, args: string) => {
    setStatusMessage(null); // Clear previous status messages
    const fullCommand = `/${command}${args ? ` ${args}` : ''}`;

    switch (command) {
      case 'compress':
        if (!sessionId) {
          // compress requires an active session
          setStatusMessage('Error: No active session to run /compress.');
          return;
        }
        runCompress();
        break;
      case 'expose':
      case 'unexpose':
        await runExposeOrUnexpose(command, args);
        break;
      default:
        setStatusMessage(`Unknown command: ${fullCommand}`);
        break;
    }
  };

  return { runCommand };
};
