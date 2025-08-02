import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useWorkspaces } from '../hooks/WorkspaceContext';

const NewWorkspacePage: React.FC = () => {
  const [workspaceName, setWorkspaceName] = useState('');
  const navigate = useNavigate();
  const { refreshWorkspaces } = useWorkspaces();

  const handleCreateWorkspace = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const response = await fetch('/api/workspaces', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ name: workspaceName }),
      });

      if (response.ok) {
        const data = await response.json();
        refreshWorkspaces();
        navigate(`/w/${data.id}/new`); // Redirect to new session in the created workspace
      } else {
        console.error('Failed to create workspace:', response.status, response.statusText);
        alert('Failed to create workspace.');
      }
    } catch (error) {
      console.error('Error creating workspace:', error);
      alert('Error creating workspace.');
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', alignItems: 'center', height: '100vh' }}>
      <h1>Create New Workspace</h1>
      <form onSubmit={handleCreateWorkspace} style={{ display: 'flex', flexDirection: 'column', gap: '10px', width: '300px' }}>
        <label>
          Workspace Name:
          <input
            type="text"
            value={workspaceName}
            onChange={(e) => setWorkspaceName(e.target.value)}
            required
            style={{ width: '100%', padding: '8px' }}
          />
        </label>
        <button type="submit" style={{ padding: '10px', backgroundColor: '#007bff', color: 'white', border: 'none', borderRadius: '5px', cursor: 'pointer' }}>
          Create Workspace
        </button>
        <button type="button" onClick={() => navigate('/workspaces')} style={{ padding: '10px', backgroundColor: '#6c757d', color: 'white', border: 'none', borderRadius: '5px', cursor: 'pointer' }}>
          Cancel
        </button>
      </form>
    </div>
  );
};

export default NewWorkspacePage;