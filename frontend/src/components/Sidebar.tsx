import React from 'react';
import { useNavigate } from 'react-router-dom';
import LogoAnimation from './LogoAnimation'; // LogoAnimation import
import { useChat } from '../hooks/ChatContext'; // Add this import
import SessionList from './SessionList';
import { Session } from '../types/chat';

interface SidebarProps {
  sessions: Session[];
  chatSessionId: string | null;
  fetchSessions: () => void;
}

const Sidebar: React.FC<SidebarProps> = ({
  sessions,
  // setSessions, // Remove this line
  chatSessionId,
  fetchSessions,
}) => {
  const navigate = useNavigate();
  const { dispatch } = useChat();

  const updateSessionState = (sessionId: string, updateFn: (session: Session) => Session) => {
    dispatch({
      type: 'SET_SESSIONS',
      payload: sessions.map(s =>
        s.id === sessionId ? updateFn(s) : s
      ),
    });
  };

  const handleDeleteSession = async (sessionId: string) => {
    try {
      await fetch(`/api/chat/deleteSession/${sessionId}`, {
        method: 'DELETE',
      });
      fetchSessions();
      if (chatSessionId === sessionId) {
        navigate('/new');
      }
    } catch (error) {
      console.error('Error deleting session:', error);
    }
  };

  return (
    <div style={{ width: '200px', background: '#f0f0f0', padding: '10px', display: 'flex', flexDirection: 'column', alignItems: 'center', borderRight: '1px solid #ccc', boxSizing: 'border-box', overflowY: 'hidden', flexShrink: 0 }}>
      <div style={{ marginBottom: '20px' }}><LogoAnimation width="50px" height="50px" color="#007bff" /></div>
      <button onClick={() => navigate('/new')} style={{ width: '100%', padding: '10px', marginBottom: '10px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>New Session</button>
      <div style={{ width: '100%', marginTop: '0px', borderTop: '1px solid #eee', paddingTop: '0px', flexGrow: 1, overflowY: 'auto' }}>
        {sessions && sessions.length === 0 ? (
          <p>No sessions yet.</p>
        ) : (
          <SessionList
            sessions={sessions}
            chatSessionId={chatSessionId}
            fetchSessions={fetchSessions}
            updateSessionState={updateSessionState}
            handleDeleteSession={handleDeleteSession}
          />
        )}
      </div>
      <div style={{ width: '100%', borderTop: '1px solid #eee', paddingTop: '10px', marginTop: '10px' }}>
        <button onClick={() => navigate('/settings')} style={{ width: '100%', padding: '10px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>Settings</button>
      </div>
    </div>
  );
};

export default Sidebar;