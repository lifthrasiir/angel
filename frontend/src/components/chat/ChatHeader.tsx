import React, { useEffect, useState } from 'react';
import { FaBars, FaChevronDown, FaChevronUp, FaLock, FaPuzzlePiece } from 'react-icons/fa';
import { useAtom, useAtomValue, useSetAtom } from 'jotai';
import { useLocation } from 'react-router-dom';
import { useSessionManagerContext } from '../../hooks/SessionManagerContext';
import { classifySessionId, getSessionId } from '../../utils/sessionStateHelpers';
import { isNewTemporarySessionURL } from '../../utils/urlSessionMapping';
import { sessionsAtom, currentSessionNameAtom } from '../../atoms/chatAtoms';
import { isSessionConfigOpenAtom } from '../../atoms/uiAtoms';
import type { Workspace } from '../../types/chat';
import SessionMenu from '../sidebar/SessionMenu';
import './ChatHeader.css';

interface ChatHeaderProps {
  workspaces: Workspace[];
  onSessionRename?: (sessionId: string) => void;
  onSessionDelete?: (sessionId: string) => void;
  onToggleSidebar?: () => void;
  children?: React.ReactNode;
}

const ChatHeader: React.FC<ChatHeaderProps> = ({
  workspaces,
  onSessionRename,
  onSessionDelete,
  onToggleSidebar,
  children,
}) => {
  const sessions = useAtomValue(sessionsAtom);
  const currentSessionName = useAtomValue(currentSessionNameAtom);
  const setSessions = useSetAtom(sessionsAtom);
  const [isSessionConfigOpen, setIsSessionConfigOpen] = useAtom(isSessionConfigOpenAtom);
  const sessionManager = useSessionManagerContext();
  const sessionId = getSessionId(sessionManager.sessionState);
  const location = useLocation();
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
  // Use currentSessionName if available (from InitialState or EventSessionName), otherwise fall back to session list or "New Chat"
  const sessionName = currentSessionName || currentSession?.name || 'New Chat';
  const currentWorkspaceId = currentSession?.workspace_id || '';

  // Determine session type
  const sessionType = classifySessionId(sessionId);
  // Also check if current URL is a temporary session URL (for sessions not yet created)
  const isTempURL = isNewTemporarySessionURL(location.pathname);

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
      <div className="chat-header-content">
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
        <div
          className="chat-header-title"
          onClick={() => setIsSessionConfigOpen(!isSessionConfigOpen)}
          style={{ cursor: 'pointer' }}
          title="Click to toggle session configuration"
        >
          {(sessionType === 'temp' || isTempURL) && (
            <FaLock className="chat-header-icon" title="Temporary session, deleted after 48 hours of inactivity" />
          )}
          {sessionType === 'internal' && (
            <FaPuzzlePiece className="chat-header-icon" title="Internal session created by system/subagent" />
          )}
          <span>{sessionName}</span>
          {isSessionConfigOpen ? (
            <FaChevronUp className="chat-header-icon" style={{ fontSize: '12px' }} />
          ) : (
            <FaChevronDown className="chat-header-icon" style={{ fontSize: '12px' }} />
          )}
        </div>

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
      {children}
    </div>
  );
};

export default ChatHeader;
