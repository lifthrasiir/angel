import { useAtomValue, useSetAtom } from 'jotai';
import { statusMessageAtom, workspaceIdAtom, sessionsAtom } from '../atoms/chatAtoms';
import { fetchSessions } from '../utils/sessionManager';

export const useCommandProcessor = (sessionId: string | null) => {
  const setStatusMessage = useSetAtom(statusMessageAtom);
  const workspaceId = useAtomValue(workspaceIdAtom);
  const setSessions = useSetAtom(sessionsAtom);

  const runCompress = async () => {
    setStatusMessage('Compressing chat history...');
    try {
      const response = await fetch(`/api/chat/${sessionId}/compress`, {
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

  const runExposeOrUnexpose = async (command: string, args: string) => {
    setStatusMessage(`Updating exposed directories...`);
    try {
      let roots: string[] = [];
      if (args) {
        roots = args
          .split(',')
          .map((path) => path.trim())
          .filter((path) => path.length > 0);
      } else {
        // Placeholder for directory selection dialog
        setStatusMessage(
          `Please provide directories as arguments for /${command} (e.g., /${command} /path/to/dir1, /path/to/dir2)`,
        );
        return;
      }

      const sessionResponse = await fetch(`/api/chat/${sessionId}`);
      if (!sessionResponse.ok) {
        const errorData = await sessionResponse.json();
        throw new Error(errorData.message || 'Failed to fetch current session roots');
      }
      const sessionData = await sessionResponse.json();
      let currentRoots: string[] = sessionData.roots || [];

      if (command === 'unexpose') {
        if (roots.length === 0) {
          // If no arguments for unexpose, clear all roots
          roots = [];
        } else {
          // Remove specified roots from current roots
          const rootsToRemove = new Set(roots);
          roots = currentRoots.filter((root) => !rootsToRemove.has(root));
        }
      } else {
        // command === 'expose'
        // Add new roots to current roots, avoiding duplicates
        const newRootsSet = new Set([...currentRoots, ...roots]);
        roots = Array.from(newRootsSet);
      }

      const response = await fetch(`/api/chat/${sessionId}/roots`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ roots }),
      });

      if (!response.ok) {
        const errorData = await response.json();
        throw new Error(errorData.message || `Failed to ${command} directories`);
      }
      const result = await response.json();
      setStatusMessage(result.message || `Directories ${command}d successfully.`);

      // TODO: Trigger loadChatSession somehow

      // Refresh the session list (to update roots displayed in UI if any)
      if (workspaceId) {
        try {
          const workspaceWithSessions = await fetchSessions(workspaceId);
          setSessions(workspaceWithSessions.sessions);
        } catch (refreshError) {
          console.error('Failed to refresh sessions after roots update:', refreshError);
        }
      }
    } catch (error: any) {
      setStatusMessage(`${command} failed: ${error.message}`);
      console.error(`${command} failed:`, error);
    }
  };

  const runCommand = async (command: string, args: string) => {
    setStatusMessage(null); // Clear previous status messages
    const fullCommand = `/${command}${args ? ` ${args}` : ''}`;

    if (!sessionId) {
      setStatusMessage('Error: No active session to run commands.');
      return;
    }

    switch (command) {
      case 'compress':
        runCompress();
        break;
      case 'expose':
      case 'unexpose':
        runExposeOrUnexpose(command, args);
        break;
      default:
        setStatusMessage(`Unknown command: ${fullCommand}`);
        break;
    }
  };

  return { runCommand };
};
