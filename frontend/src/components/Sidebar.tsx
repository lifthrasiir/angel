import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FaFolder, FaPlus, FaArrowLeft, FaCog } from 'react-icons/fa';
import LogoAnimation from './LogoAnimation';
import { useChat } from '../hooks/ChatContext';
import SessionList from './SessionList';
import WorkspaceList from './WorkspaceList';
import { Session, Workspace } from '../types/chat';

interface SidebarProps {
  sessions: Session[];
  chatSessionId: string | null;
  workspaceName?: string;
  workspaceId?: string;
  workspaces: Workspace[];
  refreshWorkspaces: () => Promise<void>;
}

const Sidebar: React.FC<SidebarProps> = ({
  sessions,
  chatSessionId,
  workspaceName,
  workspaceId,
  workspaces,
  refreshWorkspaces,
}) => {
  const navigate = useNavigate();
  const { dispatch } = useChat();
  // const { workspaceId } = useParams<{ workspaceId?: string }>(); // Remove this line
  const [showWorkspaces, setShowWorkspaces] = useState(false);

  const updateSessionState = (sessionId: string, updateFn: (session: Session) => Session) => {
    dispatch({
      type: 'SET_SESSIONS',
      payload: sessions.map((s) => (s.id === sessionId ? updateFn(s) : s)),
    });
  };

  const handleDeleteSession = async (sessionId: string) => {
    try {
      await fetch(`/api/chat/${sessionId}`, {
        method: 'DELETE',
      });
      dispatch({
        type: 'SET_SESSIONS',
        payload: sessions.filter((s) => s.id !== sessionId),
      });
      if (chatSessionId === sessionId) {
        navigate('/new');
      }
    } catch (error) {
      console.error('Error deleting session:', error);
    }
  };

  return (
    <div
      style={{
        width: '200px',
        background: '#f0f0f0',
        padding: '10px',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        borderRight: '1px solid #ccc',
        boxSizing: 'border-box',
        overflowY: 'hidden',
        flexShrink: 0,
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
        }}
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
      <button
        onClick={() => navigate(showWorkspaces ? '/w/new' : workspaceId ? `/w/${workspaceId}/new` : '/new')}
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
          border: '0',
          padding: '5px',
          backgroundColor: 'transparent',
        }}
      >
        <FaPlus style={{ marginRight: '5px' }} />
        {showWorkspaces ? 'New Workspace' : 'New Session'}
      </button>

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
              navigate(id ? `/w/${id}/new` : '/new');
              setShowWorkspaces(false);
            }}
            workspaces={workspaces}
            refreshWorkspaces={refreshWorkspaces}
          />
        ) : sessions && sessions.length === 0 ? (
          <p>No sessions yet.</p>
        ) : (
          <SessionList
            sessions={sessions}
            chatSessionId={chatSessionId}
            updateSessionState={updateSessionState}
            handleDeleteSession={handleDeleteSession}
          />
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
        onClick={() => navigate('/settings')}
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
          border: '0',
          padding: '5px',
          backgroundColor: 'transparent',
        }}
      >
        <FaCog style={{ marginRight: '5px' }} />
        Settings
      </button>
    </div>
  );
};

export default Sidebar;
