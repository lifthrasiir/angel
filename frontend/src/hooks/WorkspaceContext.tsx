import React, { createContext, useContext, useState, useEffect, ReactNode, useCallback } from 'react';
import { Workspace } from '../types/chat';

interface WorkspaceContextType {
  workspaces: Workspace[];
  loadingWorkspaces: boolean;
  errorWorkspaces: string | null;
  refreshWorkspaces: () => Promise<void>;
}

const WorkspaceContext = createContext<WorkspaceContextType | undefined>(undefined);

export const WorkspaceProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [loadingWorkspaces, setLoadingWorkspaces] = useState<boolean>(true);
  const [errorWorkspaces, setErrorWorkspaces] = useState<string | null>(null);

  const fetchAllWorkspaces = useCallback(async () => {
    setLoadingWorkspaces(true);
    setErrorWorkspaces(null);
    try {
      const response = await fetch('/api/workspaces');
      if (response.ok) {
        const data: Workspace[] = await response.json();
        setWorkspaces(data);
      } else {
        console.error('Failed to fetch workspaces:', response.status, response.statusText);
        setErrorWorkspaces('Failed to load workspaces.');
      }
    } catch (error) {
      console.error('Error fetching workspaces:', error);
      setErrorWorkspaces('Error loading workspaces.');
    } finally {
      setLoadingWorkspaces(false);
    }
  }, []);

  useEffect(() => {
    fetchAllWorkspaces();
  }, [fetchAllWorkspaces]);

  const refreshWorkspaces = useCallback(async () => {
    await fetchAllWorkspaces();
  }, [fetchAllWorkspaces]);

  return (
    <WorkspaceContext.Provider
      value={{
        workspaces,
        loadingWorkspaces,
        errorWorkspaces,
        refreshWorkspaces,
      }}
    >
      {children}
    </WorkspaceContext.Provider>
  );
};

export const useWorkspaces = () => {
  const context = useContext(WorkspaceContext);
  if (context === undefined) {
    throw new Error('useWorkspaces must be used within a WorkspaceProvider');
  }
  return context;
};
