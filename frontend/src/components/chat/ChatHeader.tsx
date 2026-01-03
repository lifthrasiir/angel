import React, { useEffect, useState } from 'react';
import { FaBars } from 'react-icons/fa';
import { useAtomValue, useSetAtom } from 'jotai';
import { useSessionManagerContext } from '../../hooks/SessionManagerContext';
import { getSessionId } from '../../utils/sessionStateHelpers';
import { sessionsAtom } from '../../atoms/chatAtoms';
import type { Workspace } from '../../types/chat';
import SessionMenu from '../sidebar/SessionMenu';
import './ChatHeader.css';

interface ChatHeaderProps {
  workspaces: Workspace[];
  onSessionRename?: (sessionId: string) => void;
  onSessionDelete?: (sessionId: string) => void;
  onToggleSidebar?: () => void;
}

const ChatHeader: React.FC<ChatHeaderProps> = ({ workspaces, onSessionRename, onSessionDelete, onToggleSidebar }) => {
  const sessions = useAtomValue(sessionsAtom);
  const setSessions = useSetAtom(sessionsAtom);
  const sessionManager = useSessionManagerContext();
  const sessionId = getSessionId(sessionManager.sessionState);
  const [isMobile, setIsMobile] = useState(false);

  // Detect mobile viewport
  useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth <= 768);
    };

    checkMobile();
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);

  // Find current session
  const currentSession = sessionId ? sessions.find((s) => s.id === sessionId) : undefined;
  const sessionName = currentSession?.name || 'New Chat';
  const currentWorkspaceId = currentSession?.workspace_id || '';

  const handleRename = () => {
    if (onSessionRename && sessionId) {
      onSessionRename(sessionId);
    }
  };

  const handleDelete = () => {
    if (onSessionDelete && sessionId) {
      onSessionDelete(sessionId);
    }
  };

  const handleSessionMoved = () => {
    if (sessionId) {
      setSessions(sessions.filter((s) => s.id !== sessionId));
    }
  };

  const handleMenuToggle = (_sid: string, _isOpen: boolean) => {
    // Could be used for highlighting active menu
  };

  return (
    <div className="chat-header">
      {/* Left: Hamburger menu for mobile */}
      {isMobile && (
        <button
          className="chat-header-hamburger"
          onClick={onToggleSidebar}
          aria-label="Toggle sidebar"
          title="Toggle sidebar"
        >
          <FaBars size={20} />
        </button>
      )}

      {/* Center: Session name */}
      <div className="chat-header-title">{sessionName}</div>

      {/* Right: Session menu */}
      {sessionId && !sessionId.startsWith('.') && (
        <SessionMenu
          sessionId={sessionId}
          sessionName={sessionName}
          onRename={handleRename}
          onDelete={handleDelete}
          isMobile={isMobile}
          currentWorkspaceId={currentWorkspaceId}
          workspaces={workspaces}
          onSessionMoved={handleSessionMoved}
          isCurrentSession={true}
          onMenuToggle={handleMenuToggle}
        />
      )}
    </div>
  );
};

export default ChatHeader;
