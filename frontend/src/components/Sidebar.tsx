import type React from 'react';
import { useState, useEffect } from 'react';
import { apiFetch } from '../api/apiClient';
import { FaArrowLeft, FaCog, FaFolder, FaPlus, FaBars, FaTimes } from 'react-icons/fa';
import { useNavigate } from 'react-router-dom';
import { useAtom, useSetAtom } from 'jotai';
import type { Workspace } from '../types/chat';
import {
  sessionsAtom,
  chatSessionIdAtom,
  workspaceNameAtom,
  workspaceIdAtom,
  selectedFilesAtom,
  preserveSelectedFilesAtom,
} from '../atoms/chatAtoms';
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
  const [sessions, setSessions] = useAtom(sessionsAtom);
  const [chatSessionId] = useAtom(chatSessionIdAtom);
  const [workspaceName] = useAtom(workspaceNameAtom);
  const [workspaceId] = useAtom(workspaceIdAtom);
  const setSelectedFiles = useSetAtom(selectedFilesAtom);
  const setPreserveSelectedFiles = useSetAtom(preserveSelectedFilesAtom);
  const [showWorkspaces, setShowWorkspaces] = useState(false);
  const [isMobile, setIsMobile] = useState(false);
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);
  const [isDraggingOverNewSession, setIsDraggingOverNewSession] = useState(false);

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
      const newPath = showWorkspaces ? '/w/new' : workspaceId ? `/w/${workspaceId}/new` : '/new';
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
        navigate(workspaceId ? `/w/${workspaceId}/new` : '/new');
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
            fontWeight: !showWorkspaces && workspaceId ? 'bold' : '',
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
              : workspaceId
                ? `Current Workspace: ${workspaceName || 'New Workspace'}`
                : 'Show Workspaces'
          }
        >
          {showWorkspaces ? (
            <>
              <FaArrowLeft style={{ marginRight: '5px' }} />
              Back to Sessions
            </>
          ) : workspaceId ? (
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
            onClick={() => handleNavigate(showWorkspaces ? '/w/new' : workspaceId ? `/w/${workspaceId}/new` : '/new')}
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
            aria-label={showWorkspaces ? 'Create New Workspace' : 'Create New Session'}
          >
            <FaPlus style={{ marginRight: '5px' }} />
            {showWorkspaces ? 'New Workspace' : 'New Session'}
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
              currentWorkspaceId={workspaceId}
              onSelectWorkspace={(id) => {
                handleNavigate(id ? `/w/${id}/new` : '/new');
                setShowWorkspaces(false);
              }}
              workspaces={workspaces}
              refreshWorkspaces={refreshWorkspaces}
            />
          ) : sessions && sessions.length === 0 ? (
            <p>No sessions yet.</p>
          ) : (
            <SessionList handleDeleteSession={handleDeleteSession} />
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
