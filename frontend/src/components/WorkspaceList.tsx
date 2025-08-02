import React, { useEffect, useState } from 'react';
import { FaTrash } from 'react-icons/fa';
import { useNavigate } from 'react-router-dom';

interface Workspace {
  id: string;
  name: string;
  default_system_prompt: string;
  created_at: string;
}

interface WorkspaceListProps {
  currentWorkspaceId?: string;
  onSelectWorkspace: (workspaceId: string) => void;
}

const WorkspaceList: React.FC<WorkspaceListProps> = ({ currentWorkspaceId, onSelectWorkspace }) => {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const navigate = useNavigate();

  const fetchWorkspaces = async () => {
    try {
      const response = await fetch('/api/workspaces');
      if (response.ok) {
        const data: Workspace[] = await response.json();
        setWorkspaces(data);
      } else {
        console.error('Failed to fetch workspaces:', response.status, response.statusText);
      }
    } catch (error) {
      console.error('Error fetching workspaces:', error);
    }
  };

  const handleDeleteWorkspace = async (workspaceId: string) => {
    if (window.confirm('Are you sure you want to delete this workspace and all its sessions?')) {
      try {
        const response = await fetch(`/api/workspaces/${workspaceId}`, {
          method: 'DELETE',
        });
        if (response.ok) {
          fetchWorkspaces();
          if (currentWorkspaceId === workspaceId) {
            navigate('/new'); // Redirect to no-workspace new session if current workspace is deleted
          }
        } else {
          console.error('Failed to delete workspace:', response.status, response.statusText);
        }
      } catch (error) {
        console.error('Error deleting workspace:', error);
      }
    }
  };

  useEffect(() => {
    fetchWorkspaces();
  }, []);

  return (
    <ul style={{ listStyle: 'none', margin: '0', padding: '10px 0', width: '100%' }}>
      <li style={{ marginBottom: '10px', display: 'flex', alignItems: 'center' }}>
        <button
          onClick={() => onSelectWorkspace('')}
          style={{
            flexGrow: 1,
            padding: '10px',
            textAlign: 'left',
            backgroundColor: currentWorkspaceId === '' ? '#e0e0e0' : '#f9f9f9',
            border: '1px solid #ddd',
            borderRadius: '5px',
            cursor: 'pointer',
          }}
        >
          No workspace
        </button>
      </li>
      {workspaces.map((workspace) => (
        <li key={workspace.id} style={{ marginBottom: '10px', display: 'flex', alignItems: 'center' }}>
          <button
            onClick={() => onSelectWorkspace(workspace.id)}
            style={{
              flexGrow: 1,
              padding: '10px',
              textAlign: 'left',
              backgroundColor: currentWorkspaceId === workspace.id ? '#e0e0e0' : '#f9f9f9',
              border: '1px solid #ddd',
              borderRadius: '5px',
              cursor: 'pointer',
            }}
          >
            {workspace.name}
          </button>
          <button
            onClick={() => {
              if (window.confirm('Are you sure you want to delete this workspace?\nThis would remove all contained sessions as well!')) {
                handleDeleteWorkspace(workspace.id);
              }
            }}
            className="sidebar-delete-button"
          >
            <FaTrash size={16} />
          </button>
        </li>
      ))}
    </ul>
  );
};

export default WorkspaceList;
