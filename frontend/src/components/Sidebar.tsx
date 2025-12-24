import React, { useEffect, useRef, useState } from 'react';
import { apiFetch } from '../api/apiClient';
import { FaArrowLeft, FaCog, FaFolder, FaPlus, FaBars, FaTimes, FaSearch } from 'react-icons/fa';
import { useNavigate, useLocation } from 'react-router-dom';
import { useAtom, useSetAtom } from 'jotai';
import type { Workspace } from '../types/chat';
import { sessionsAtom, workspaceNameAtom, selectedFilesAtom, preserveSelectedFilesAtom } from '../atoms/chatAtoms';
import { useSessionFSM } from '../hooks/useSessionFSM';
import { extractWorkspaceId, isNewNonTemporarySessionURL } from '../utils/urlSessionMapping';
import { fetchSessions } from '../utils/sessionManager';
import LogoAnimation from './LogoAnimation';
import SessionList from './SessionList';
import WorkspaceList from './WorkspaceList';
import { extractFilesFromDrop } from '../utils/dragDropUtils';

interface SidebarProps {
  workspaces: Workspace[];
  refreshWorkspaces: () => Promise<void>;
}

const Sidebar: React.FC<SidebarProps> = ({ workspaces, refreshWorkspaces }) => {
  const navigate = useNavigate();
  const location = useLocation();
  const [sessions, setSessions] = useAtom(sessionsAtom);

  // Use the new unified useSessionFSM hook
  const sessionFSM = useSessionFSM();
  const { sessionId: chatSessionId, workspaceId: sessionWorkspaceId } = sessionFSM;

  const [workspaceName, setWorkspaceName] = useAtom(workspaceNameAtom);
  const setSelectedFiles = useSetAtom(selectedFilesAtom);
  const setPreserveSelectedFiles = useSetAtom(preserveSelectedFilesAtom);
  const [showWorkspaces, setShowWorkspaces] = useState(false);
  const [isMobile, setIsMobile] = useState(false);
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);
  const [isDraggingOverNewSession, setIsDraggingOverNewSession] = useState(false);

  // Separate state for UI active workspace (decoupled from session's workspace)
  const [activeWorkspaceId, setActiveWorkspaceId] = useState<string | undefined>(undefined);
  const isInitializedRef = useRef(false);

  // Helper function to check if current path represents anonymous workspace (not workspaces)
  const isAnonymousWorkspacePath = (pathname: string): boolean => {
    return pathname === '/new' || pathname === '/' || pathname === '/temp';
  };

  // Check if we're on a new non-temporary session page, to adjust "New Session" button label
  const showTemporarySessionButton = isNewNonTemporarySessionURL(location.pathname);

  // Initialize activeWorkspaceId on first load or when session loads
  useEffect(() => {
    const urlWorkspaceId = extractWorkspaceId(location.pathname);

    // Always handle URL-based workspace changes (including anonymous paths)
    if (urlWorkspaceId) {
      // URL has workspace info (/w/:workspaceId/new)
      setActiveWorkspaceId(urlWorkspaceId);
      if (!isInitializedRef.current) {
        isInitializedRef.current = true;
      }
    } else if (isAnonymousWorkspacePath(location.pathname)) {
      // Explicitly on global/anonymous workspace (including temporary sessions)
      setActiveWorkspaceId('');
      if (!isInitializedRef.current) {
        isInitializedRef.current = true;
      }
    } else if (!isInitializedRef.current && sessionWorkspaceId !== undefined) {
      // Only wait for sessionWorkspaceId during initial load (/:sessionId case)
      setActiveWorkspaceId(sessionWorkspaceId || '');
      isInitializedRef.current = true;
    }
  }, [location.pathname, sessionWorkspaceId, setActiveWorkspaceId]);

  // Reset activeWorkspaceId when a new session is loaded
  // This copies the workspaceId from session state to sidebar state
  useEffect(() => {
    const urlWorkspaceId = extractWorkspaceId(location.pathname);
    const isSessionPath = chatSessionId && !urlWorkspaceId && location.pathname !== '/new';

    // When navigating to an existing session, update activeWorkspaceId from session's workspace
    if (isSessionPath && sessionWorkspaceId !== undefined) {
      setActiveWorkspaceId(sessionWorkspaceId || '');
      // Mark as initialized once we have session workspace info
      if (!isInitializedRef.current) {
        isInitializedRef.current = true;
      }
    }
  }, [chatSessionId, sessionWorkspaceId, location.pathname, setActiveWorkspaceId]);

  // Load sessions when activeWorkspaceId changes
  // This is the ONLY place where session list should be loaded
  useEffect(() => {
    if (!isInitializedRef.current) {
      return;
    }

    // Don't load if activeWorkspaceId is undefined (still initializing)
    if (activeWorkspaceId === undefined) {
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
        setIsSidebarOpen(false); // Close sidebar on desktop
      }
    };

    checkMobile();
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);

  // Close sidebar when navigating on mobile
  const handleNavigate = (path: string) => {
    navigate(path);
    if (isMobile) {
      setIsSidebarOpen(false);
    }
  };

  // Handle workspace navigation when current session moves to different workspace
  const handleNavigateToWorkspace = (newWorkspaceId: string) => {
    // Update active workspace explicitly (this will trigger session reload via useEffect)
    setActiveWorkspaceId(newWorkspaceId);
    navigate(`/w/${newWorkspaceId}/new`);
  };

  // Handle workspace selection from WorkspaceList
  const handleSelectWorkspace = (workspaceId: string) => {
    // Update active workspace explicitly
    setActiveWorkspaceId(workspaceId || undefined);
    navigate(workspaceId ? `/w/${workspaceId}/new` : '/new');
    setShowWorkspaces(false);
    if (isMobile) {
      setIsSidebarOpen(false);
    }
  };

  const handleNewSessionDrop = async (e: React.DragEvent<HTMLButtonElement>) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDraggingOverNewSession(false);

    const filesToAdd = await extractFilesFromDrop(e);

    // Set files and navigate to new session
    if (filesToAdd.length > 0) {
      setSelectedFiles(filesToAdd);
      setPreserveSelectedFiles(filesToAdd);

      // Navigate to new session
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
    setIsDraggingOverNewSession(true);
  };

  const handleDragLeave = (e: React.DragEvent<HTMLButtonElement>) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDraggingOverNewSession(false);
  };

  const handleDeleteSession = async (sessionId: string) => {
    try {
      await apiFetch(`/api/chat/${sessionId}`, {
        method: 'DELETE',
      });
      setSessions(sessions.filter((s) => s.id !== sessionId));
      if (chatSessionId === sessionId) {
        navigate(activeWorkspaceId ? `/w/${activeWorkspaceId}/new` : '/new');
      }
    } catch (error) {
      console.error('Error deleting session:', error);
    }
  };

  return (
    <>
      {/* Mobile hamburger button */}
      {isMobile && (
        <button
          onClick={() => setIsSidebarOpen(!isSidebarOpen)}
          style={{
            position: 'fixed',
            top: '10px',
            left: '10px',
            zIndex: 1001,
            background: '#f0f0f0',
            border: '1px solid #ccc',
            borderRadius: '8px',
            padding: '10px',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            minWidth: '44px',
            minHeight: '44px',
          }}
          aria-label="Toggle menu"
        >
          {isSidebarOpen ? <FaTimes /> : <FaBars />}
        </button>
      )}

      {/* Mobile overlay */}
      {isMobile && isSidebarOpen && (
        <div
          onClick={() => setIsSidebarOpen(false)}
          style={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            background: 'rgba(0, 0, 0, 0.5)',
            zIndex: 999,
          }}
        />
      )}

      {/* Sidebar */}
      <div
        style={{
          width: 'var(--sidebar-width)',
          background: '#f0f0f0',
          padding: '10px',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          borderRight: '1px solid #ccc',
          boxSizing: 'border-box',
          overflowY: 'hidden',
          flexShrink: 0,
          position: isMobile ? 'fixed' : 'relative',
          top: isMobile ? 0 : 'auto',
          left: isMobile ? (isSidebarOpen ? 0 : '-100%') : 'auto',
          height: isMobile ? '100vh' : 'auto',
          zIndex: 1000,
          transition: isMobile ? 'left 0.3s ease-in-out' : 'none',
        }}
      >
        <div style={{ marginBottom: '20px' }}>
          <LogoAnimation width="50px" height="50px" color="#007bff" />
        </div>
        <button
          onClick={() => setShowWorkspaces(!showWorkspaces)}
          style={{
            width: '100%',
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            cursor: 'pointer',
            color: 'black',
            textDecoration: 'none',
            textAlign: 'left',
            fontWeight: !showWorkspaces && activeWorkspaceId ? 'bold' : '',
            display: 'flex',
            alignItems: 'center',
            border: '0',
            padding: '5px',
            backgroundColor: 'transparent',
            minHeight: 'var(--touch-target-size)',
          }}
          aria-label={
            showWorkspaces
              ? 'Back to Sessions'
              : activeWorkspaceId
                ? `Current Workspace: ${workspaceName || 'New Workspace'}`
                : 'Show Workspaces'
          }
        >
          {showWorkspaces ? (
            <>
              <FaArrowLeft style={{ marginRight: '5px' }} />
              Back to Sessions
            </>
          ) : activeWorkspaceId ? (
            <>
              <FaFolder style={{ marginRight: '5px' }} />
              {workspaceName || 'New Workspace'}
            </>
          ) : (
            <>
              <FaFolder style={{ marginRight: '5px' }} />
              Workspaces
            </>
          )}
        </button>
        <hr
          style={{
            width: '100%',
            height: '1px',
            border: '0',
            backgroundColor: '#ccc',
          }}
        />
        <div style={{ position: 'relative', width: '100%' }}>
          {isDraggingOverNewSession && (
            <div
              style={{
                position: 'absolute',
                top: 0,
                left: 0,
                right: 0,
                bottom: 0,
                backgroundColor: 'rgba(0, 123, 255, 0.1)',
                border: '2px solid rgba(0, 123, 255, 0.3)',
                borderRadius: '4px',
                pointerEvents: 'none',
                zIndex: 1,
              }}
            />
          )}
          <button
            onClick={() => {
              if (showWorkspaces) {
                handleNavigate('/w/new');
              } else {
                handleNavigate(
                  (activeWorkspaceId ? `/w/${activeWorkspaceId}` : '') +
                    (showTemporarySessionButton ? '/temp' : '/new'),
                );
              }
            }}
            onDrop={handleNewSessionDrop}
            onDragOver={handleDragOver}
            onDragLeave={handleDragLeave}
            style={{
              width: '100%',
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              cursor: 'pointer',
              color: 'black',
              textDecoration: 'none',
              textAlign: 'left',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'flex-start',
              border: '0',
              padding: '5px',
              backgroundColor: 'transparent',
              minHeight: 'var(--touch-target-size)',
            }}
            aria-label={
              showWorkspaces
                ? 'Create New Workspace'
                : showTemporarySessionButton
                  ? 'Create New Temporary Session'
                  : 'Create New Session'
            }
          >
            <FaPlus style={{ marginRight: '5px' }} />
            {showWorkspaces ? 'New Workspace' : showTemporarySessionButton ? 'New Temporary Session' : 'New Session'}
          </button>
        </div>

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
            <p>No sessions yet.</p>
          ) : (
            <SessionList
              handleDeleteSession={handleDeleteSession}
              onSessionSelect={(sessionId) => handleNavigate(`/${sessionId}`)}
              workspaces={workspaces}
              onSessionMoved={(movedSessionId: string) => {
                // Remove the moved session from local state for immediate UI update
                setSessions(sessions.filter((s) => s.id !== movedSessionId));
              }}
              onNavigateToWorkspace={handleNavigateToWorkspace}
              activeWorkspaceId={activeWorkspaceId}
            />
          )}
        </div>
        <hr
          style={{
            width: '100%',
            height: '1px',
            border: '0',
            backgroundColor: '#ccc',
          }}
        />
        <button
          onClick={() => handleNavigate('/search')}
          style={{
            width: '100%',
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            cursor: 'pointer',
            color: 'black',
            textDecoration: 'none',
            textAlign: 'left',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'flex-start',
            border: '0',
            padding: '5px',
            backgroundColor: 'transparent',
            minHeight: 'var(--touch-target-size)',
          }}
          aria-label="Go to Search"
        >
          <FaSearch style={{ marginRight: '5px' }} />
          Search
        </button>
        <button
          onClick={() => handleNavigate('/settings')}
          style={{
            width: '100%',
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            cursor: 'pointer',
            color: 'black',
            textDecoration: 'none',
            textAlign: 'left',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'flex-start',
            border: '0',
            padding: '5px',
            backgroundColor: 'transparent',
            minHeight: 'var(--touch-target-size)',
          }}
          aria-label="Go to Settings"
        >
          <FaCog style={{ marginRight: '5px' }} />
          Settings
        </button>
      </div>
    </>
  );
};

export default Sidebar;
