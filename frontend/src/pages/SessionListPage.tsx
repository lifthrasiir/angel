import type React from 'react';
import { useEffect, useState, useCallback } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { apiFetch } from '../api/apiClient';
import { FaPlus, FaClock, FaSpinner, FaLock, FaArchive } from 'react-icons/fa';
import type { SessionWithDetails } from '../types/chat';
import './SessionListPage.css';

const SessionListPage: React.FC = () => {
  const navigate = useNavigate();
  const { workspaceId } = useParams();
  const [sessions, setSessions] = useState<SessionWithDetails[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const loadSessions = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const url = workspaceId ? `/api/sessions?workspaceId=${workspaceId}` : '/api/sessions';
      const response = await apiFetch(url);
      if (!response.ok) {
        throw new Error('Failed to load sessions');
      }
      const data: SessionWithDetails[] = await response.json();
      setSessions(data);
    } catch (err: any) {
      console.error('Failed to load sessions:', err);
      setError(err.message || 'Failed to load sessions');
    } finally {
      setIsLoading(false);
    }
  }, [workspaceId]);

  useEffect(() => {
    loadSessions();
  }, [loadSessions]);

  const formatDate = (dateString?: string) => {
    if (!dateString) return '';
    return new Date(dateString).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
    });
  };

  const handleNewSession = () => {
    if (workspaceId) {
      navigate(`/w/${workspaceId}/new`);
    } else {
      navigate('/new');
    }
  };

  const handleNewTempSession = () => {
    if (workspaceId) {
      navigate(`/w/${workspaceId}/temp`);
    } else {
      navigate('/temp');
    }
  };

  const handleSessionClick = (sessionId: string) => {
    navigate(`/${sessionId}`);
  };

  return (
    <div className="session-list-page-container">
      {/* Header */}
      <div className="session-list-page-header">
        <h1 className="session-list-page-title">{workspaceId ? 'Workspace Sessions' : 'All Sessions'}</h1>
        <div className="session-list-page-actions">
          <button onClick={handleNewTempSession} className="session-list-page-button temp-button">
            <FaPlus />
            New Temporary Session
          </button>
          <button onClick={handleNewSession} className="session-list-page-button primary-button">
            <FaPlus />
            New Session
          </button>
        </div>
      </div>

      {/* Loading State */}
      {isLoading && (
        <div className="session-list-page-loading">
          <FaSpinner className="animate-spin" size={32} />
          <p>Loading sessions...</p>
        </div>
      )}

      {/* Error State */}
      {error && !isLoading && (
        <div className="session-list-page-error">
          <p>{error}</p>
          <button onClick={loadSessions} className="session-list-page-retry-button">
            Retry
          </button>
        </div>
      )}

      {/* Empty State */}
      {!isLoading && !error && sessions.length === 0 && (
        <div className="session-list-page-empty">
          <FaClock size={48} style={{ marginBottom: '16px', opacity: 0.5 }} />
          <p>No sessions yet</p>
          <p className="session-list-page-empty-hint">Create a new session to get started</p>
        </div>
      )}

      {/* Session List */}
      {!isLoading && !error && sessions.length > 0 && (
        <div className="session-list-page-list">
          {sessions.map((session) => {
            const isTemporary = session.id.startsWith('.');
            const isArchived = session.archived;
            const firstDate = formatDate(session.first_message_at || session.created_at);
            const lastDate = formatDate(session.last_updated_at);
            return (
              <div key={session.id} onClick={() => handleSessionClick(session.id)} className="session-list-page-item">
                <div className="session-list-page-item-header">
                  <h3 className="session-list-page-item-name">
                    {isTemporary && <FaLock className="session-list-page-temp-icon" />}
                    {isArchived && <FaArchive className="session-list-page-temp-icon" title="Archived" />}
                    {session.name || 'Untitled Session'}
                  </h3>
                  <span className="session-list-page-item-date">
                    {firstDate}
                    {session.first_message_at && firstDate !== lastDate && ` ~ ${lastDate}`}
                  </span>
                </div>
                {session.last_message_text && (
                  <p className="session-list-page-item-preview">{session.last_message_text}</p>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
};

export default SessionListPage;
