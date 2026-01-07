import type React from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';

const SettingsPage: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();

  // Determine active tab based on current path
  const getActiveTab = () => {
    const pathSegments = location.pathname.split('/');
    if (pathSegments[2] === 'prompts') return 'prompts';
    return pathSegments[2] || 'auth';
  };

  const activeTab = getActiveTab();

  return (
    <div style={{ display: 'flex', height: '100vh', width: '100%' }}>
      {/* Settings Sidebar */}
      <div
        style={{
          width: '150px',
          background: '#f0f0f0',
          padding: '20px',
          borderRight: '1px solid #ccc',
        }}
      >
        <h2 style={{ marginBottom: '20px' }}>Settings</h2>
        <ul style={{ listStyle: 'none', padding: 0 }}>
          <li style={{ marginBottom: '10px' }}>
            <button
              onClick={() => navigate('/settings/auth')}
              style={{
                width: '100%',
                padding: '10px',
                textAlign: 'left',
                background: activeTab === 'auth' ? '#e0e0e0' : 'none',
                border: 'none',
                borderRadius: '5px',
                cursor: 'pointer',
              }}
            >
              Authentication
            </button>
          </li>
          <li style={{ marginBottom: '10px' }}>
            <button
              onClick={() => navigate('/settings/mcp')}
              style={{
                width: '100%',
                padding: '10px',
                textAlign: 'left',
                background: activeTab === 'mcp' ? '#e0e0e0' : 'none',
                border: 'none',
                borderRadius: '5px',
                cursor: 'pointer',
              }}
            >
              MCP
            </button>
          </li>
          <li style={{ marginBottom: '10px' }}>
            <button
              onClick={() => navigate('/settings/openai')}
              style={{
                width: '100%',
                padding: '10px',
                textAlign: 'left',
                background: activeTab === 'openai' ? '#e0e0e0' : 'none',
                border: 'none',
                borderRadius: '5px',
                cursor: 'pointer',
              }}
            >
              OpenAI API
            </button>
          </li>
          <li style={{ marginBottom: '10px' }}>
            <button
              onClick={() => navigate('/settings/prompts')}
              style={{
                width: '100%',
                padding: '10px',
                textAlign: 'left',
                background: activeTab === 'prompts' ? '#e0e0e0' : 'none',
                border: 'none',
                borderRadius: '5px',
                cursor: 'pointer',
              }}
            >
              System Prompts
            </button>
          </li>
        </ul>
        <div
          style={{
            marginTop: '20px',
            paddingTop: '10px',
            borderTop: '1px solid #ccc',
          }}
        >
          <button
            onClick={() => navigate('/new')}
            style={{
              width: '100%',
              padding: '10px',
              textAlign: 'left',
              background: 'none',
              border: 'none',
              borderRadius: '5px',
              cursor: 'pointer',
              color: '#007bff',
            }}
          >
            ← Back to Chat
          </button>
        </div>
      </div>

      {/* Settings Content */}
      <div style={{ flexGrow: 1, padding: '20px', overflowY: 'auto' }}>
        <Outlet />
      </div>
    </div>
  );
};

export default SettingsPage;
