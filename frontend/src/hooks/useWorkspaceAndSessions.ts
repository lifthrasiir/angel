import { useEffect, useState } from 'react';
import { useSetAtom } from 'jotai';
import type { Workspace } from '../types/chat';
import { fetchSessions } from '../utils/sessionManager';
import { sessionsAtom } from '../atoms/chatAtoms';

interface UseWorkspaceAndSessionsResult {
  currentWorkspace: Workspace | null;
  // sessions: Session[]; // sessions will be managed by Jotai
  loading: boolean;
  error: string | null;
}

export const useWorkspaceAndSessions = (workspaceIdFromState: string | undefined): UseWorkspaceAndSessionsResult => {
  const [currentWorkspace, setCurrentWorkspace] = useState<Workspace | null>(null);
  const setSessions = useSetAtom(sessionsAtom);
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
        setCurrentWorkspace(null);
        setSessions([]);
        setLoading(false);
      }
    };

    loadData();
  }, [workspaceIdFromState, setSessions]);

  return { currentWorkspace, loading, error };
};
