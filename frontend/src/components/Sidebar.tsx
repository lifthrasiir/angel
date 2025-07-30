import React from 'react';
import { useNavigate } from 'react-router-dom';
import LogoAnimation from './LogoAnimation'; // LogoAnimation import
import { FaEdit } from 'react-icons/fa';
import { Session } from '../hooks/useChatSession';

interface SidebarProps {
  sessions: Session[];
  setSessions: React.Dispatch<React.SetStateAction<Session[]>>;
  chatSessionId: string | null;
  fetchSessions: () => void;
}

const Sidebar: React.FC<SidebarProps> = ({
  sessions,
  setSessions,
  chatSessionId,
  fetchSessions,
}) => {
  const navigate = useNavigate();

  return (
    <div style={{ width: '200px', background: '#f0f0f0', padding: '10px', display: 'flex', flexDirection: 'column', alignItems: 'center', borderRight: '1px solid #ccc', boxSizing: 'border-box', overflowY: 'hidden', flexShrink: 0 }}>
      <div style={{ marginBottom: '20px' }}><LogoAnimation width="50px" height="50px" color="#007bff" /></div>
      <button onClick={() => navigate('/new')} style={{ width: '100%', padding: '10px', marginBottom: '10px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>New Session</button>
      <div style={{ width: '100%', marginTop: '0px', borderTop: '1px solid #eee', paddingTop: '0px', flexGrow: 1, overflowY: 'auto' }}>
        {sessions && sessions.length === 0 ? (
          <p>No sessions yet.</p>
        ) : (
          <ul style={{ listStyle: 'none', padding: 0, width: '100%' }}>
            {sessions.map((session) => (
              <li key={session.id} style={{ marginBottom: '5px', display: 'flex', alignItems: 'center' }}>
                {session.isEditing ? (
                  <input
                    type="text"
                    value={session.name || ''}
                    onChange={(e) => {
                      setSessions(prevSessions =>
                        prevSessions.map(s =>
                          s.id === session.id ? { ...s, name: e.target.value } : s
                        )
                      );
                    }}
                    onBlur={async () => {
                      if (session.id) {
                        try {
                          await fetch('/api/chat/updateSessionName', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ sessionId: session.id, name: session.name || '' }),
                          });
                          fetchSessions();
                        } catch (error) {
                          console.error('Error updating session name:', error);
                        }
                      }
                      setSessions(prevSessions =>
                        prevSessions.map(s =>
                          s.id === session.id ? { ...s, isEditing: false } : s
                        )
                      );
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.currentTarget.blur();
                      }
                    }}
                    style={{ flexGrow: 1, padding: '8px', border: '1px solid #ddd', borderRadius: '5px' }}
                  />
                ) : (
                  <button
                    onClick={() => navigate(`/${session.id}`)}
                    style={{
                      flexGrow: 1,
                      padding: '8px',
                      textAlign: 'left',
                      border: '1px solid #ddd',
                      borderRadius: '5px',
                      background: session.id === chatSessionId ? '#e0e0e0' : 'white',
                      cursor: 'pointer',
                      whiteSpace: 'nowrap',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                    }}
                  >
                    {session.name || 'New Chat'}
                  </button>
                )}
                <button
                  onClick={() => {
                    setSessions(prevSessions =>
                      prevSessions.map(s =>
                        s.id === session.id ? { ...s, isEditing: true } : s
                      )
                    );
                  }}
                  style={{
                    marginLeft: '5px',
                    padding: '5px',
                    background: 'none',
                    border: 'none',
                    cursor: 'pointer',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    color: '#555',
                  }}
                >
                  <FaEdit size={16} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
      <div style={{ width: '100%', borderTop: '1px solid #eee', paddingTop: '10px', marginTop: '10px' }}>
        <button onClick={() => navigate('/settings')} style={{ width: '100%', padding: '10px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>Settings</button>
      </div>
    </div>
  );
};

export default Sidebar;