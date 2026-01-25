import React, { useEffect, useRef, useState } from 'react';
import { apiFetch } from '../../api/apiClient';
import { useNavigate, useLocation } from 'react-router-dom';
import { useAtom, useSetAtom } from 'jotai';
import type { Workspace } from '../../types/chat';
import { sessionsAtom } from '../../atoms/chatAtoms';
import { workspaceNameAtom } from '../../atoms/workspaceAtoms';
import { selectedFilesAtom, preserveSelectedFilesAtom } from '../../atoms/fileAtoms';
import { useSessionFSM } from '../../hooks/useSessionFSM';
import { extractWorkspaceId, isNewNonTemporarySessionURL } from '../../utils/urlSessionMapping';
import { fetchSessions } from '../../utils/sessionManager';
import SessionList from './SessionList';
import WorkspaceList from './WorkspaceList';
import SidebarHeader from './SidebarHeader';
import NewSessionButton from './NewSessionButton';
import SidebarNavigation from './SidebarNavigation';
import SidebarMobile from './SidebarMobile';
import { extractFilesFromDrop } from '../../utils/dragDropUtils';

interface SidebarProps {
  workspaces: Workspace[];
  refreshWorkspaces: () => Promise<void>;
  isMobileSidebarOpen?: boolean;
  onSetMobileSidebarOpen?: (open: boolean) => void;
}

const Sidebar: React.FC<SidebarProps> = ({
  workspaces,
  refreshWorkspaces,
  isMobileSidebarOpen,
  onSetMobileSidebarOpen,
}) => {
  const navigate = useNavigate();
  const location = useLocation();
  const [sessions, setSessions] = useAtom(sessionsAtom);

  const sessionFSM = useSessionFSM();
  const { sessionId: chatSessionId, workspaceId: sessionWorkspaceId } = sessionFSM;

  const [workspaceName, setWorkspaceName] = useAtom(workspaceNameAtom);
  const setSelectedFiles = useSetAtom(selectedFilesAtom);
  const setPreserveSelectedFiles = useSetAtom(preserveSelectedFilesAtom);

  const [showWorkspaces, setShowWorkspaces] = useState(false);
  const [isMobile, setIsMobile] = useState(false);
  const [isSidebarOpen, setIsSidebarOpen] = useState(isMobileSidebarOpen !== undefined ? isMobileSidebarOpen : false);

  // Sync with external state
  useEffect(() => {
    if (isMobileSidebarOpen !== undefined) {
      setIsSidebarOpen(isMobileSidebarOpen);
    }
  }, [isMobileSidebarOpen]);

  const [activeWorkspaceId, setActiveWorkspaceId] = useState<string | undefined>(undefined);
  const isInitializedRef = useRef(false);

  const isAnonymousWorkspacePath = (pathname: string): boolean => {
    return pathname === '/new' || pathname === '/' || pathname === '/temp' || pathname === '/all';
  };

  const showTemporarySessionButton = isNewNonTemporarySessionURL(location.pathname);

  // Initialize activeWorkspaceId
  useEffect(() => {
    const urlWorkspaceId = extractWorkspaceId(location.pathname);

    if (urlWorkspaceId) {
      setActiveWorkspaceId(urlWorkspaceId);
      if (!isInitializedRef.current) {
        isInitializedRef.current = true;
      }
    } else if (isAnonymousWorkspacePath(location.pathname)) {
      setActiveWorkspaceId('');
      if (!isInitializedRef.current) {
        isInitializedRef.current = true;
      }
    } else if (!isInitializedRef.current && sessionWorkspaceId !== undefined) {
      setActiveWorkspaceId(sessionWorkspaceId || '');
      isInitializedRef.current = true;
    }
  }, [location.pathname, sessionWorkspaceId]);

  // Reset activeWorkspaceId when session loads
  useEffect(() => {
    const urlWorkspaceId = extractWorkspaceId(location.pathname);
    const isSessionPath = chatSessionId && !urlWorkspaceId && location.pathname !== '/new';

    if (isSessionPath && sessionWorkspaceId !== undefined) {
      setActiveWorkspaceId(sessionWorkspaceId || '');
      if (!isInitializedRef.current) {
        isInitializedRef.current = true;
      }
    }
  }, [chatSessionId, sessionWorkspaceId, location.pathname]);

  // Load sessions when activeWorkspaceId changes
  useEffect(() => {
    if (!isInitializedRef.current || activeWorkspaceId === undefined) {
      return;
    }

    const loadSessionsForWorkspace = async () => {
      try {
        const result = await fetchSessions(activeWorkspaceId);
        setSessions(result.sessions);
        if (result.workspace) {
          setWorkspaceName(result.workspace.name);
        } else {
          setWorkspaceName('');
        }
      } catch (error) {
        console.error('Failed to load sessions for workspace:', error);
      }
    };

    loadSessionsForWorkspace();
  }, [activeWorkspaceId, setSessions, setWorkspaceName]);

  // Detect mobile screen size
  useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth <= 768);
      if (window.innerWidth > 768) {
        setIsSidebarOpen(false);
      }
    };

    checkMobile();
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);

  const handleNavigate = (path: string) => {
    navigate(path);
    if (isMobile) {
      setIsSidebarOpen(false);
      onSetMobileSidebarOpen?.(false);
    }
  };

  const handleNavigateToWorkspace = (newWorkspaceId: string) => {
    setActiveWorkspaceId(newWorkspaceId);
    navigate(`/w/${newWorkspaceId}/new`);
  };

  const handleSelectWorkspace = (workspaceId: string) => {
    setActiveWorkspaceId(workspaceId || undefined);
    navigate(workspaceId ? `/w/${workspaceId}/new` : '/new');
    setShowWorkspaces(false);
    if (isMobile) {
      setIsSidebarOpen(false);
      onSetMobileSidebarOpen?.(false);
    }
  };

  const handleNewSessionDrop = async (e: React.DragEvent<HTMLButtonElement>) => {
    e.preventDefault();
    e.stopPropagation();

    const filesToAdd = await extractFilesFromDrop(e);

    if (filesToAdd.length > 0) {
      setSelectedFiles(filesToAdd);
      setPreserveSelectedFiles(filesToAdd);

      let newPath: string;
      if (showWorkspaces) {
        newPath = '/w/new';
      } else {
        newPath =
          (activeWorkspaceId ? `/w/${activeWorkspaceId}` : '') + (showTemporarySessionButton ? '/temp' : '/new');
      }
      handleNavigate(newPath);
    }
  };

  const handleDragOver = (e: React.DragEvent<HTMLButtonElement>) => {
    e.preventDefault();
    e.stopPropagation();
    e.dataTransfer.dropEffect = 'copy';
  };

  const handleDragLeave = (e: React.DragEvent<HTMLButtonElement>) => {
    e.preventDefault();
    e.stopPropagation();
  };

  const handleDeleteSession = async (sessionId: string) => {
    try {
      await apiFetch(`/api/chat/${sessionId}`, { method: 'DELETE' });
      setSessions(sessions.filter((s) => s.id !== sessionId));
      if (chatSessionId === sessionId) {
        navigate(activeWorkspaceId ? `/w/${activeWorkspaceId}/new` : '/new');
      }
    } catch (error) {
      console.error('Error deleting session:', error);
    }
  };

  const handleNewSessionClick = () => {
    if (showWorkspaces) {
      handleNavigate('/w/new');
    } else {
      handleNavigate(
        (activeWorkspaceId ? `/w/${activeWorkspaceId}` : '') + (showTemporarySessionButton ? '/temp' : '/new'),
      );
    }
  };

  const sidebarContent = (
    <div
      style={{
        width: 'var(--sidebar-width)',
        background: '#f0f0f0',
        padding: '10px 5px',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        borderRight: '1px solid #ccc',
        boxSizing: 'border-box',
        overflowY: 'hidden',
        flexShrink: 0,
      }}
    >
      <SidebarHeader
        showWorkspaces={showWorkspaces}
        workspaceName={workspaceName ?? ''}
        activeWorkspaceId={activeWorkspaceId}
        onToggleView={() => setShowWorkspaces(!showWorkspaces)}
      />

      <NewSessionButton
        showWorkspaces={showWorkspaces}
        showTemporarySessionButton={showTemporarySessionButton}
        onClick={handleNewSessionClick}
        onDrop={handleNewSessionDrop}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
      />

      <div
        style={{
          width: '100%',
          marginTop: '0px',
          borderTop: '1px solid #eee',
          paddingTop: '0px',
          flexGrow: 1,
          overflowY: 'auto',
        }}
      >
        {showWorkspaces ? (
          <WorkspaceList
            currentWorkspaceId={activeWorkspaceId}
            onSelectWorkspace={handleSelectWorkspace}
            workspaces={workspaces}
            refreshWorkspaces={refreshWorkspaces}
          />
        ) : sessions && sessions.length === 0 ? (
          <p style={{ padding: '0 5px' }}>No sessions yet.</p>
        ) : (
          <SessionList
            handleDeleteSession={handleDeleteSession}
            onSessionSelect={(sessionId) => handleNavigate(`/${sessionId}`)}
            workspaces={workspaces}
            onSessionMoved={(movedSessionId: string) => {
              setSessions(sessions.filter((s) => s.id !== movedSessionId));
            }}
            onNavigateToWorkspace={handleNavigateToWorkspace}
            activeWorkspaceId={activeWorkspaceId}
          />
        )}
      </div>

      <SidebarNavigation onNavigate={handleNavigate} />
    </div>
  );

  if (isMobile) {
    return (
      <SidebarMobile
        isOpen={isSidebarOpen}
        onOverlayClick={() => {
          setIsSidebarOpen(false);
          onSetMobileSidebarOpen?.(false);
        }}
      >
        {sidebarContent}
      </SidebarMobile>
    );
  }

  return sidebarContent;
};

export default Sidebar;
