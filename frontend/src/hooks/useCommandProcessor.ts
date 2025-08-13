import { useAtomValue, useSetAtom } from 'jotai';
import { statusMessageAtom, workspaceIdAtom, sessionsAtom } from '../atoms/chatAtoms';
import { fetchSessions } from '../utils/sessionManager';

export const useCommandProcessor = (sessionId: string | null) => {
  const setStatusMessage = useSetAtom(statusMessageAtom);
  const workspaceId = useAtomValue(workspaceIdAtom);
  const setSessions = useSetAtom(sessionsAtom);

  const runCommand = async (command: string, args: string) => {
    setStatusMessage(null); // Clear previous status messages
    const fullCommand = `/${command}${args ? ` ${args}` : ''}`;

    if (!sessionId) {
      setStatusMessage('Error: No active session to run commands.');
      return;
    }

    switch (command) {
      case 'compress':
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
        break;
      default:
        setStatusMessage(`Unknown command: ${fullCommand}`);
        break;
    }
  };

  return { runCommand };
};
