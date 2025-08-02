import React from 'react';
import { useNavigate } from 'react-router-dom';
import { FaEdit, FaTrash } from 'react-icons/fa';
import { Session } from '../types/chat';

interface SessionListProps {
  sessions: Session[];
  chatSessionId: string | null;
  fetchSessions: () => void;
  updateSessionState: (sessionId: string, updateFn: (session: Session) => Session) => void;
  handleDeleteSession: (sessionId: string) => Promise<void>;
}

const SessionList: React.FC<SessionListProps> = ({
  sessions,
  chatSessionId,
  fetchSessions,
  updateSessionState,
  handleDeleteSession,
}) => {
  const navigate = useNavigate();

  return (
    <ul style={{ listStyle: 'none', padding: 0, width: '100%' }}>
      {sessions.map((session) => (
        <li key={session.id} className="sidebar-session-item">
          {session.isEditing ? (
            <div className="sidebar-session-edit-container">
              <input
                type="text"
                value={session.name || ''}
                onChange={(e) => {
                  updateSessionState(session.id, s => ({ ...s, name: e.target.value }));
                }}
                onBlur={async () => {
                  if (session.id) {
                    try {
                      await fetch(`/api/chat/${session.id}/name`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ sessionId: session.id, name: session.name || '' }),
                      });
                      fetchSessions();
                    } catch (error) {
                      console.error('Error updating session name:', error);
                    }
                  }
                  updateSessionState(session.id, s => ({ ...s, isEditing: false }));
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.currentTarget.blur();
                  } else if (e.key === 'Escape') {
                    updateSessionState(session.id, s => ({ ...s, isEditing: false }));
                  }
                }}
                className="sidebar-session-name-input"
              />
              <button
                onClick={() => {
                  if (session.id && window.confirm('Are you sure you want to delete this session?')) {
                    handleDeleteSession(session.id);
                  }
                }}
                className="sidebar-delete-button"
              >
                <FaTrash size={16} />
              </button>
            </div>
          ) : (
            <button
              onClick={() => navigate(`/${session.id}`)}
              className={`sidebar-session-button ${session.id === chatSessionId ? 'active' : ''}`}
            >
              {session.name || 'New Chat'}
            </button>
          )}
          {!session.isEditing && (
            <button
              onClick={() => {
                updateSessionState(session.id, s => ({ ...s, isEditing: true }));
              }}
              className="sidebar-edit-button"
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
