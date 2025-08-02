import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import MCPSettings from '../components/MCPSettings'; // Import the new component

const SettingsPage: React.FC = () => {
  const [activeTab, setActiveTab] = useState('auth');
  const [userEmail, setUserEmail] = useState<string | null>(null);
  const isLoggedIn = !!userEmail; // userEmail이 있으면 로그인된 것으로 간주
  const navigate = useNavigate();

  useEffect(() => {
    document.title = 'Angel: Settings';
    const fetchUserInfo = async () => {
      try {
        const response = await fetch('/api/userinfo');
        if (response.ok) {
          const data = await response.json();
          setUserEmail(data.email);
        } else if (response.status === 401) {
          setUserEmail(null);
        } else {
          console.error('Failed to fetch user info:', response.status, response.statusText);
          setUserEmail(null);
        }
      } catch (error) {
        console.error('Error fetching user info:', error);
        setUserEmail(null);
      }
    };
    fetchUserInfo();
  }, []);

  const handleLogout = async () => {
    try {
      const response = await fetch('/api/logout', { method: 'POST' });
      if (response.ok) {
        setUserEmail(null);
        // Optionally redirect to login page or home
        window.location.href = '/'; // Redirect to home after logout
      } else {
        console.error('Failed to logout:', response.status, response.statusText);
      }
    } catch (error) {
      console.error('Error during logout:', error);
    }
  };

  const handleLogin = () => {
    const currentPath = window.location.pathname + window.location.search;
    let redirectToUrl = `/login?redirect_to=${encodeURIComponent(currentPath)}`;
    window.location.href = redirectToUrl;
  };

  return (
    <div style={{ display: 'flex', height: '100vh', width: '100%' }}>
      {/* Settings Sidebar/Header */}
      <div style={{ width: '150px', background: '#f0f0f0', padding: '20px', borderRight: '1px solid #ccc' }}>
        <h2 style={{ marginBottom: '20px' }}>Settings</h2>
        <ul style={{ listStyle: 'none', padding: 0 }}>
          <li style={{ marginBottom: '10px' }}>
            <button 
              onClick={() => setActiveTab('auth')}
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
              onClick={() => setActiveTab('mcp')}
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
        </ul>
        <div style={{ marginTop: '20px', paddingTop: '10px', borderTop: '1px solid #ccc' }}>
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
        {activeTab === 'auth' && (
          <div>
            <h3>Authentication</h3>
            {isLoggedIn ? (
              <p>
                Logged in as: <strong>{userEmail}</strong>
                <button onClick={handleLogout} style={{ marginLeft: '10px', padding: '5px 10px' }}>Logout</button>
              </p>
            ) : (
              <p>
                Not logged in.
                <button onClick={handleLogin} style={{ marginLeft: '10px', padding: '5px 10px' }}>Login</button>
              </p>
            )}
          </div>
        )}
        {activeTab === 'mcp' && <MCPSettings />}
      </div>
    </div>
  );
};

export default SettingsPage;