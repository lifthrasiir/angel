import { useEffect, useState } from 'react';
import { Workspace, Session } from '../types/chat';
import { fetchSessions } from '../utils/sessionManager';

interface UseWorkspaceAndSessionsResult {
  currentWorkspace: Workspace | null;
  sessions: Session[];
  loading: boolean;
  error: string | null;
}

// This hook now takes the workspaceId as a prop
export const useWorkspaceAndSessions = (workspaceIdFromState: string | undefined): UseWorkspaceAndSessionsResult => {
  const [currentWorkspace, setCurrentWorkspace] = useState<Workspace | null>(null);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const loadData = async () => {
      setLoading(true);
      setError(null);

      // workspaceIdFromState가 undefined가 아닐 때만 fetchSessions를 호출합니다.
      if (workspaceIdFromState !== undefined) {
        try {
          const workspaceWithSessions = await fetchSessions(workspaceIdFromState);
          setCurrentWorkspace(workspaceWithSessions.workspace);
          setSessions(workspaceWithSessions.sessions);
        } catch (err) {
          console.error('Failed to fetch workspace or sessions:', err);
          setError('Failed to load workspace or sessions.');
        } finally {
          setLoading(false);
        }
      } else {
        // workspaceIdFromState가 undefined일 경우 로딩 상태를 즉시 해제하고 세션을 비웁니다.
        setCurrentWorkspace(null);
        setSessions([]);
        setLoading(false);
      }
    };

    loadData();
  }, [workspaceIdFromState]); // Depend on the passed workspaceId

  return { currentWorkspace, sessions, loading, error };
};
