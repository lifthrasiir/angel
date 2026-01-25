import type React from 'react';
import { useEffect, useState, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { FaList } from 'react-icons/fa';
import { apiFetch } from '../../api/apiClient';
import { useAtom, useSetAtom } from 'jotai';
import type { Session, Workspace } from '../../types/chat';
import { sessionsAtom, currentSessionNameAtom } from '../../atoms/chatAtoms';
import { selectedFilesAtom, preserveSelectedFilesAtom } from '../../atoms/fileAtoms';
import { useSessionManagerContext } from '../../hooks/SessionManagerContext';
import { getSessionId } from '../../utils/sessionStateHelpers';
import { extractFilesFromDrop } from '../../utils/dragDropUtils';
import SessionMenu from './SessionMenu';
import './SessionList.css';

interface SessionListProps {
  handleDeleteSession: (sessionId: string) => Promise<void>;
  onSessionSelect: (sessionId: string) => void;
  workspaces: Workspace[];
  onSessionMoved?: (sessionId: string) => void;
  onNavigateToWorkspace?: (workspaceId: string) => void;
  activeWorkspaceId?: string; // UI active workspace (for SessionMenu)
}

const SessionList: React.FC<SessionListProps> = ({
  handleDeleteSession,
  onSessionSelect,
  workspaces,
  onSessionMoved,
  onNavigateToWorkspace,
  activeWorkspaceId,
}) => {
  const navigate = useNavigate();
  const [sessions, setSessions] = useAtom(sessionsAtom);
  // Use sessionManager for current session ID
  const sessionManager = useSessionManagerContext();
  const chatSessionId = getSessionId(sessionManager.sessionState);
  const [selectedFiles, setSelectedFiles] = useAtom(selectedFilesAtom);
  const setCurrentSessionName = useSetAtom(currentSessionNameAtom);
  const setPreserveSelectedFiles = useSetAtom(preserveSelectedFilesAtom);
  const [draggedSessionId, setDraggedSessionId] = useState<string | null>(null);
  const [isDraggingOver, setIsDraggingOver] = useState(false);
  const [isMobile, setIsMobile] = useState(false);
  const [openMenuSessionId, setOpenMenuSessionId] = useState<string | null>(null);
  const inputRefs = useRef<{ [key: string]: HTMLInputElement | null }>({});
  // Temporary state for edit input - only applied on Enter
  const [editingSessionId, setEditingSessionId] = useState<string | null>(null);
  const [tempEditValue, setTempEditValue] = useState<string>('');

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
    const session = sessions.find((s) => s.id === sessionId);
    if (session) {
      setEditingSessionId(sessionId);
      setTempEditValue(session.name || '');
    }

    // Focus the input after the component re-renders
    queueMicrotask(() => {
      if (inputRefs.current[sessionId]) {
        inputRefs.current[sessionId]?.focus();
        inputRefs.current[sessionId]?.select();
      }
    });
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
      <li className="sidebar-session-list-all-link">
        <button
          onClick={() => navigate(activeWorkspaceId ? `/w/${activeWorkspaceId}/all` : '/all')}
          className="sidebar-session-button"
          title="View all sessions"
        >
          <FaList className="sidebar-list-icon" />
          All Sessions
        </button>
      </li>
      {sessions.map((session) => (
        <li key={session.id} className={`sidebar-session-item ${session.id === openMenuSessionId ? 'active' : ''}`}>
          {editingSessionId === session.id ? (
            <div className="sidebar-session-edit-container">
              <input
                ref={(el) => {
                  if (el) {
                    inputRefs.current[session.id] = el;
                  }
                }}
                type="text"
                value={tempEditValue}
                onChange={(e) => {
                  setTempEditValue(e.target.value);
                }}
                onBlur={() => {
                  // On blur, just cancel editing (don't apply changes)
                  setEditingSessionId(null);
                  setTempEditValue('');
                  delete inputRefs.current[session.id];
                }}
                onKeyDown={async (e) => {
                  if (e.key === 'Enter') {
                    // Apply changes on Enter
                    if (session.id) {
                      try {
                        await apiFetch(`/api/chat/${session.id}/name`, {
                          method: 'POST',
                          headers: { 'Content-Type': 'application/json' },
                          body: JSON.stringify({ name: tempEditValue || '' }),
                        });
                        // Update local state only after successful API call
                        updateSessionState(session.id, (s) => ({
                          ...s,
                          name: tempEditValue,
                        }));
                        // If this is the current session, also update currentSessionName
                        if (session.id === chatSessionId) {
                          setCurrentSessionName(tempEditValue);
                        }
                      } catch (error) {
                        console.error('Error updating session name:', error);
                      }
                    }
                    setEditingSessionId(null);
                    setTempEditValue('');
                    delete inputRefs.current[session.id];
                  } else if (e.key === 'Escape') {
                    // Cancel on Escape
                    setEditingSessionId(null);
                    setTempEditValue('');
                    delete inputRefs.current[session.id];
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
              currentWorkspaceId={activeWorkspaceId || ''}
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
