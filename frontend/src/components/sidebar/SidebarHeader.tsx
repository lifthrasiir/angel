import React from 'react';
import { FaArrowLeft, FaFolder } from 'react-icons/fa';
import LogoAnimation from '../LogoAnimation';

interface SidebarHeaderProps {
  showWorkspaces: boolean;
  workspaceName: string;
  activeWorkspaceId: string | undefined;
  onToggleView: () => void;
}

export const SidebarHeader: React.FC<SidebarHeaderProps> = ({
  showWorkspaces,
  workspaceName,
  activeWorkspaceId,
  onToggleView,
}) => {
  return (
    <>
      <div style={{ marginBottom: '20px' }}>
        <LogoAnimation width="50px" height="50px" color="#007bff" />
      </div>
      <button
        onClick={onToggleView}
        style={{
          width: '100%',
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          cursor: 'pointer',
          color: 'black',
          textDecoration: 'none',
          textAlign: 'left',
          fontWeight: !showWorkspaces && activeWorkspaceId ? 'bold' : '',
          display: 'flex',
          alignItems: 'center',
          border: '0',
          padding: '5px',
          backgroundColor: 'transparent',
          minHeight: 'var(--touch-target-size)',
        }}
        aria-label={
          showWorkspaces
            ? 'Back to Sessions'
            : activeWorkspaceId
              ? `Current Workspace: ${workspaceName || 'New Workspace'}`
              : 'Show Workspaces'
        }
      >
        {showWorkspaces ? (
          <>
            <FaArrowLeft style={{ marginRight: '5px' }} />
            Back to Sessions
          </>
        ) : activeWorkspaceId ? (
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
    </>
  );
};

export default SidebarHeader;
