import type React from 'react';
import { useEffect, useState } from 'react';
import { apiFetch } from '../api/apiClient';
import { useAtom, useSetAtom } from 'jotai';
import type { Session, Workspace } from '../types/chat';
import {
  sessionsAtom,
  chatSessionIdAtom,
  selectedFilesAtom,
  preserveSelectedFilesAtom,
  workspaceIdAtom,
} from '../atoms/chatAtoms';
import { extractFilesFromDrop } from '../utils/dragDropUtils';
import SessionMenu from './SessionMenu';

interface SessionListProps {
  handleDeleteSession: (sessionId: string) => Promise<void>;
  onSessionSelect: (sessionId: string) => void;
  workspaces: Workspace[];
  onSessionMoved?: (sessionId: string) => void;
  onNavigateToWorkspace?: (workspaceId: string) => void;
}

const SessionList: React.FC<SessionListProps> = ({
  handleDeleteSession,
  onSessionSelect,
  workspaces,
  onSessionMoved,
  onNavigateToWorkspace,
}) => {
  const [sessions, setSessions] = useAtom(sessionsAtom);
  const [chatSessionId] = useAtom(chatSessionIdAtom);
  const [workspaceId] = useAtom(workspaceIdAtom);
  const [selectedFiles, setSelectedFiles] = useAtom(selectedFilesAtom);
  const setPreserveSelectedFiles = useSetAtom(preserveSelectedFilesAtom);
  const [draggedSessionId, setDraggedSessionId] = useState<string | null>(null);
  const [isDraggingOver, setIsDraggingOver] = useState(false);
  const [isMobile, setIsMobile] = useState(false);
  const [openMenuSessionId, setOpenMenuSessionId] = useState<string | null>(null);

  // Detect mobile viewport
  useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth <= 768);
    };

    checkMobile();
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);

  const updateSessionState = (sessionId: string, updateFn: (session: Session) => Session) => {
    setSessions(sessions.map((s) => (s.id === sessionId ? updateFn(s) : s)));
  };

  // Handle rename action from menu
  const handleRenameSession = (sessionId: string) => {
    updateSessionState(sessionId, (s) => ({
      ...s,
      isEditing: true,
    }));
  };

  // Handle delete action from menu
  const handleDeleteFromMenu = (sessionId: string) => {
    if (window.confirm('Are you sure you want to delete this session?')) {
      handleDeleteSession(sessionId);
    }
  };

  // Handle menu toggle to highlight active session
  const handleMenuToggle = (sessionId: string, isOpen: boolean) => {
    setOpenMenuSessionId(isOpen ? sessionId : null);
  };

  // Handle file drop on session button
  const handleFileDrop = async (e: React.DragEvent<HTMLButtonElement>, sessionId: string) => {
    e.preventDefault();
    e.stopPropagation();
    setDraggedSessionId(null);
    setIsDraggingOver(false);

    const filesToAdd = await extractFilesFromDrop(e);

    // Handle files differently based on target session
    if (filesToAdd.length > 0) {
      if (sessionId === chatSessionId) {
        // Add to existing files for current session
        setSelectedFiles((prevFiles) => [...prevFiles, ...filesToAdd]);
      } else {
        // Replace files for different session
        setSelectedFiles(filesToAdd);
        // Set preserve files to ensure they survive navigation
        setPreserveSelectedFiles(filesToAdd);
        onSessionSelect(sessionId);
      }
    }
  };

  const handleDragOver = (e: React.DragEvent<HTMLButtonElement>) => {
    e.preventDefault();
    e.stopPropagation();
    e.dataTransfer.dropEffect = 'copy';
    setIsDraggingOver(true);
  };

  const handleDragEnter = (e: React.DragEvent<HTMLButtonElement>, sessionId: string) => {
    e.preventDefault();
    e.stopPropagation();
    setDraggedSessionId(sessionId);
    setIsDraggingOver(true);
  };

  const handleDragLeave = (e: React.DragEvent<HTMLButtonElement>) => {
    e.preventDefault();
    e.stopPropagation();
    // Only set dragging to false if we're actually leaving the button
    if (e.currentTarget === e.target) {
      setIsDraggingOver(false);
      setDraggedSessionId(null);
    }
  };

  return (
    <ul
      style={{
        listStyle: 'none',
        margin: '0',
        padding: '5px 0',
        width: '100%',
      }}
    >
      {sessions.map((session) => (
        <li
          key={session.id}
          className="sidebar-session-item"
          style={{
            backgroundColor: openMenuSessionId === session.id ? '#f0f8ff' : 'transparent',
            border: openMenuSessionId === session.id ? '1px solid #007bff' : '1px solid transparent',
            borderRadius: '4px',
            transition: 'all 0.2s ease',
          }}
        >
          {session.isEditing ? (
            <div className="sidebar-session-edit-container">
              <input
                type="text"
                value={session.name || ''}
                onChange={(e) => {
                  updateSessionState(session.id, (s) => ({
                    ...s,
                    name: e.target.value,
                  }));
                }}
                onBlur={async () => {
                  if (session.id) {
                    try {
                      await apiFetch(`/api/chat/${session.id}/name`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ name: session.name || '' }),
                      });
                    } catch (error) {
                      console.error('Error updating session name:', error);
                    }
                  }
                  updateSessionState(session.id, (s) => ({
                    ...s,
                    isEditing: false,
                  }));
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.currentTarget.blur();
                  } else if (e.key === 'Escape') {
                    updateSessionState(session.id, (s) => ({
                      ...s,
                      isEditing: false,
                    }));
                  }
                }}
                className="sidebar-session-name-input"
                aria-label="Edit session name"
              />
            </div>
          ) : (
            <button
              onClick={() => onSessionSelect(session.id)}
              onDrop={(e) => handleFileDrop(e, session.id)}
              onDragOver={handleDragOver}
              onDragEnter={(e) => handleDragEnter(e, session.id)}
              onDragLeave={handleDragLeave}
              className={`sidebar-session-button ${session.id === chatSessionId ? 'active' : ''} ${draggedSessionId === session.id ? 'drag-over' : ''}`}
              style={{
                position: 'relative',
                transition: 'all 0.2s ease-in-out',
              }}
              title={session.name || 'New Chat'}
              aria-label={`Go to ${session.name || 'New Chat'} session`}
            >
              {isDraggingOver && draggedSessionId === session.id && (
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
              {session.name || 'New Chat'}
              {selectedFiles.length > 0 && session.id === chatSessionId && (
                <span
                  style={{
                    position: 'absolute',
                    top: '2px',
                    right: '2px',
                    backgroundColor: '#007bff',
                    color: 'white',
                    borderRadius: '50%',
                    width: '16px',
                    height: '16px',
                    fontSize: '10px',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    fontWeight: 'bold',
                  }}
                >
                  {selectedFiles.length}
                </span>
              )}
            </button>
          )}
          {!session.isEditing && (
            <SessionMenu
              sessionId={session.id}
              sessionName={session.name || 'New Chat'}
              onRename={handleRenameSession}
              onDelete={handleDeleteFromMenu}
              isMobile={isMobile}
              currentWorkspaceId={workspaceId || ''}
              workspaces={workspaces}
              onSessionMoved={() => onSessionMoved && onSessionMoved(session.id)}
              isCurrentSession={session.id === chatSessionId}
              onNavigateToWorkspace={onNavigateToWorkspace}
              onMenuToggle={handleMenuToggle}
            />
          )}
        </li>
      ))}
    </ul>
  );
};

export default SessionList;
