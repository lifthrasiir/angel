import type React from 'react';
import { FaEdit, FaTrash } from 'react-icons/fa';
import { useNavigate } from 'react-router-dom';
import { useAtom } from 'jotai';
import type { Session } from '../types/chat';
import { sessionsAtom, chatSessionIdAtom } from '../atoms/chatAtoms';

interface SessionListProps {
  handleDeleteSession: (sessionId: string) => Promise<void>;
}

const SessionList: React.FC<SessionListProps> = ({ handleDeleteSession }) => {
  const navigate = useNavigate();
  const [sessions, setSessions] = useAtom(sessionsAtom);
  const [chatSessionId] = useAtom(chatSessionIdAtom);

  const updateSessionState = (sessionId: string, updateFn: (session: Session) => Session) => {
    setSessions(sessions.map((s) => (s.id === sessionId ? updateFn(s) : s)));
  };

  return (
    <ul
      style={{
        listStyle: 'none',
        margin: '0',
        padding: '10px 0',
        width: '100%',
      }}
    >
      {sessions.map((session) => (
        <li key={session.id} className="sidebar-session-item">
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
                      await fetch(`/api/chat/${session.id}/name`, {
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
              <button
                onClick={() => {
                  if (window.confirm('Are you sure you want to delete this session?')) {
                    handleDeleteSession(session.id);
                  }
                }}
                className="sidebar-delete-button"
                aria-label="Delete session"
              >
                <FaTrash size={16} />
              </button>
            </div>
          ) : (
            <button
              onClick={() => navigate(`/${session.id}`)}
              className={`sidebar-session-button ${session.id === chatSessionId ? 'active' : ''}`}
              title={session.name || 'New Chat'}
              aria-label={`Go to ${session.name || 'New Chat'} session`}
            >
              {session.name || 'New Chat'}
            </button>
          )}
          {!session.isEditing && (
            <button
              onClick={() => {
                updateSessionState(session.id, (s) => ({
                  ...s,
                  isEditing: true,
                }));
              }}
              className="sidebar-edit-button"
              aria-label="Change session name or delete session"
            >
              <FaEdit size={16} />
            </button>
          )}
        </li>
      ))}
    </ul>
  );
};

export default SessionList;
