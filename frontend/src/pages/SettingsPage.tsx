import type React from 'react';
import { useEffect, useState } from 'react';
import { apiFetch } from '../api/apiClient';
import { useNavigate } from 'react-router-dom';
import { useAtom } from 'jotai';
import MCPSettings from '../components/MCPSettings'; // Import the new component
import SystemPromptEditor, { PredefinedPrompt } from '../components/SystemPromptEditor'; // Import SystemPromptEditor and PredefinedPrompt type

import { globalPromptsAtom } from '../atoms/chatAtoms';

const SettingsPage: React.FC = () => {
  const [activeTab, setActiveTab] = useState('auth');
  const [userEmail, setUserEmail] = useState<string | null>(null);
  const [globalPrompts, setGlobalPrompts] = useAtom(globalPromptsAtom); // Use setGlobalPrompts
  const isLoggedIn = !!userEmail; // userEmail이 있으면 로그인된 것으로 간주
  const navigate = useNavigate();

  // State for managing global prompts
  const [editingPrompt, setEditingPrompt] = useState<PredefinedPrompt | null>(null);
  const [newPromptLabel, setNewPromptLabel] = useState('');
  const [newPromptValue, setNewPromptValue] = useState('');
  const [isAddingNewPrompt, setIsAddingNewPrompt] = useState(false);

  const fetchGlobalPrompts = async () => {
    console.log('fetchGlobalPrompts: Fetching global prompts...');
    try {
      const response = await apiFetch('/api/systemPrompts');
      if (response.ok) {
        const data: PredefinedPrompt[] = await response.json();
        setGlobalPrompts(data);
        console.log('fetchGlobalPrompts: Fetched prompts:', data);
      } else {
        console.error('fetchGlobalPrompts: Failed to fetch global prompts:', response.status, response.statusText);
      }
    } catch (error) {
      console.error('fetchGlobalPrompts: Error fetching global prompts:', error);
    }
  };

  useEffect(() => {
    document.title = 'Angel: Settings';

    const fetchUserInfo = async () => {
      try {
        const response = await apiFetch('/api/userinfo');
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
    fetchGlobalPrompts();
  }, []);

  const handleLogout = async () => {
    try {
      const response = await apiFetch('/api/logout', { method: 'POST' });
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
    const redirectToUrl = `/login?redirect_to=${encodeURIComponent(currentPath)}`;
    window.location.href = redirectToUrl;
  };

  // Handlers for global prompts
  const savePromptsToBackend = async (prompts: PredefinedPrompt[]) => {
    console.log('savePromptsToBackend: Saving prompts:', prompts);
    try {
      const response = await apiFetch('/api/systemPrompts', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(prompts),
      });

      if (!response.ok) {
        console.error('savePromptsToBackend: Failed to save global prompts:', response.status, response.statusText);
        alert('Failed to save prompts. Please try again.');
        return false;
      }
      console.log('savePromptsToBackend: Prompts saved successfully.');
      return true;
    } catch (error) {
      console.error('savePromptsToBackend: Error saving global prompts:', error);
      alert('Error saving prompts. Please check your connection.');
      return false;
    }
  };

  const handleSavePrompt = async () => {
    let updatedPrompts: PredefinedPrompt[] = [];

    if (isAddingNewPrompt) {
      if (newPromptLabel.trim() === '' || newPromptValue.trim() === '') {
        alert('Label and prompt content cannot be empty.');
        return;
      }
      // Check for duplicate label
      if (globalPrompts.some((p) => p.label === newPromptLabel)) {
        alert('A prompt with this label already exists. Please use a unique label.');
        return;
      }
      const newPrompt: PredefinedPrompt = { label: newPromptLabel, value: newPromptValue };
      updatedPrompts = [...globalPrompts, newPrompt];
    } else if (editingPrompt) {
      if (newPromptLabel.trim() === '' || newPromptValue.trim() === '') {
        alert('Label and prompt content cannot be empty.');
        return;
      }
      // Check for duplicate label, excluding the current editing prompt
      if (globalPrompts.some((p) => p.label === newPromptLabel && p.label !== editingPrompt.label)) {
        alert('A prompt with this label already exists. Please use a unique label.');
        return;
      }
      updatedPrompts = globalPrompts.map((p) =>
        p.label === editingPrompt.label ? { label: newPromptLabel, value: newPromptValue } : p,
      );
    }

    const success = await savePromptsToBackend(updatedPrompts);
    if (success) {
      setGlobalPrompts(updatedPrompts); // Update Jotai atom only after successful API call
      // Reset state after saving
      setEditingPrompt(null);
      setNewPromptLabel('');
      setNewPromptValue('');
      setIsAddingNewPrompt(false);
    }
  };

  const handleDeletePrompt = async (promptToDelete: PredefinedPrompt) => {
    console.log('handleDeletePrompt: Attempting to delete prompt:', promptToDelete.label);
    if (window.confirm(`Are you sure you want to delete the prompt "${promptToDelete.label}"?`)) {
      const updatedPrompts = globalPrompts.filter((p) => p.label !== promptToDelete.label);
      console.log('handleDeletePrompt: Prompts after filter:', updatedPrompts);
      const success = await savePromptsToBackend(updatedPrompts);
      if (success) {
        setGlobalPrompts(updatedPrompts); // Update Jotai atom
        console.log('handleDeletePrompt: Jotai atom updated with:', updatedPrompts);
        if (updatedPrompts.length === 0) {
          console.log('handleDeletePrompt: All prompts deleted, re-fetching defaults...');
          await fetchGlobalPrompts();
        }
        // If the deleted prompt was being edited, clear editing state
        if (editingPrompt && editingPrompt.label === promptToDelete.label) {
          setEditingPrompt(null);
          setNewPromptLabel('');
          setNewPromptValue('');
          setIsAddingNewPrompt(false);
        }
      }
    }
  };

  const handleEditPrompt = (prompt: PredefinedPrompt) => {
    setEditingPrompt(prompt);
    setNewPromptLabel(prompt.label);
    setNewPromptValue(prompt.value);
    setIsAddingNewPrompt(false);
  };

  const handleAddNewPrompt = () => {
    setEditingPrompt(null);
    setNewPromptLabel(''); // Clear for new prompt
    setNewPromptValue(''); // Clear for new prompt
    setIsAddingNewPrompt(true);
  };

  const handleMoveUp = async (index: number) => {
    if (index === 0) return; // Cannot move up if it's the first item
    const updatedPrompts = [...globalPrompts];
    [updatedPrompts[index], updatedPrompts[index - 1]] = [updatedPrompts[index - 1], updatedPrompts[index]]; // Swap
    setGlobalPrompts(updatedPrompts);
    await savePromptsToBackend(updatedPrompts);
  };

  const handleMoveDown = async (index: number) => {
    if (index === globalPrompts.length - 1) return; // Cannot move down if it's the last item
    const updatedPrompts = [...globalPrompts];
    [updatedPrompts[index], updatedPrompts[index + 1]] = [updatedPrompts[index + 1], updatedPrompts[index]]; // Swap
    setGlobalPrompts(updatedPrompts);
    await savePromptsToBackend(updatedPrompts);
  };

  const handleCancelEdit = () => {
    setEditingPrompt(null);
    setNewPromptLabel('');
    setNewPromptValue('');
    setIsAddingNewPrompt(false);
  };

  interface AuthSettingsProps {
    isLoggedIn: boolean;
    userEmail: string | null;
    handleLogout: () => void;
    handleLogin: () => void;
  }

  const AuthSettings: React.FC<AuthSettingsProps> = ({ isLoggedIn, userEmail, handleLogout, handleLogin }) => {
    return (
      <div>
        <h3>Authentication</h3>
        {isLoggedIn ? (
          <p>
            Logged in as: <strong>{userEmail}</strong>
            <button onClick={handleLogout} style={{ marginLeft: '10px', padding: '5px 10px' }}>
              Logout
            </button>
          </p>
        ) : (
          <p>
            Not logged in.
            <button onClick={handleLogin} style={{ marginLeft: '10px', padding: '5px 10px' }}>
              Login
            </button>
          </p>
        )}
      </div>
    );
  };

  interface PromptSettingsProps {
    globalPrompts: PredefinedPrompt[];
    editingPrompt: PredefinedPrompt | null;
    isAddingNewPrompt: boolean;
    newPromptLabel: string;
    newPromptValue: string;
    setNewPromptLabel: (label: string) => void;
    setNewPromptValue: (value: string) => void;
    handleSavePrompt: () => void;
    handleCancelEdit: () => void;
    handleEditPrompt: (prompt: PredefinedPrompt) => void;
    handleDeletePrompt: (prompt: PredefinedPrompt) => void;
    handleAddNewPrompt: () => void;
    handleMoveUp: (index: number) => void;
    handleMoveDown: (index: number) => void;
  }

  const PromptSettings: React.FC<PromptSettingsProps> = ({
    globalPrompts,
    editingPrompt,
    isAddingNewPrompt,
    newPromptLabel,
    newPromptValue,
    setNewPromptLabel,
    setNewPromptValue,
    handleSavePrompt,
    handleCancelEdit,
    handleEditPrompt,
    handleDeletePrompt,
    handleAddNewPrompt,
    handleMoveUp,
    handleMoveDown,
  }) => {
    return (
      <div>
        <h3>Global System Prompts</h3>
        {editingPrompt || isAddingNewPrompt ? (
          <div style={{ border: '1px solid #eee', padding: '10px', minHeight: '100px' }}>
            <h4>{isAddingNewPrompt ? 'Add New Prompt' : 'Edit Prompt'}</h4>
            <SystemPromptEditor
              initialPrompt={newPromptValue}
              currentLabel={newPromptLabel}
              onPromptUpdate={(updatedPrompt) => {
                setNewPromptLabel(updatedPrompt.label);
                setNewPromptValue(updatedPrompt.value);
              }}
              isEditing={true}
              isGlobalSettings={true}
            />
            <div style={{ marginTop: '10px' }}>
              <button onClick={handleSavePrompt} style={{ marginRight: '10px' }}>
                Save
              </button>
              <button onClick={handleCancelEdit}>Cancel</button>
            </div>
          </div>
        ) : (
          <>
            <div style={{ border: '1px solid #eee', padding: '10px', minHeight: '100px' }}>
              {globalPrompts.length === 0 ? (
                <p>No global prompts defined. Add one below!</p>
              ) : (
                <ul>
                  {globalPrompts.map((prompt, index) => (
                    <li key={prompt.label} style={{ marginBottom: '5px', display: 'flex', alignItems: 'center' }}>
                      <strong>{prompt.label}:</strong> {prompt.value.substring(0, Math.min(prompt.value.length, 50))}...
                      <button
                        onClick={() => handleEditPrompt(prompt)}
                        style={{ marginLeft: '10px', padding: '2px 8px' }}
                      >
                        Edit
                      </button>
                      <button
                        onClick={() => handleDeletePrompt(prompt)}
                        style={{ marginLeft: '5px', padding: '2px 8px' }}
                      >
                        Delete
                      </button>
                      <button
                        onClick={() => handleMoveUp(index)}
                        disabled={index === 0}
                        style={{ marginLeft: '5px', padding: '2px 8px' }}
                      >
                        Move Up
                      </button>
                      <button
                        onClick={() => handleMoveDown(index)}
                        disabled={index === globalPrompts.length - 1}
                        style={{ marginLeft: '5px', padding: '2px 8px' }}
                      >
                        Move Down
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <div style={{ marginTop: '10px' }}>
              <button onClick={handleAddNewPrompt}>Add New Prompt</button>
            </div>
          </>
        )}
      </div>
    );
  };

  return (
    <div style={{ display: 'flex', height: '100vh', width: '100%' }}>
      {/* Settings Sidebar/Header */}
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
          <li style={{ marginBottom: '10px' }}>
            <button
              onClick={() => setActiveTab('prompts')}
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
      {/* Settings Content */}
      <div style={{ flexGrow: 1, padding: '20px', overflowY: 'auto' }}>
        {activeTab === 'auth' && (
          <AuthSettings
            isLoggedIn={isLoggedIn}
            userEmail={userEmail}
            handleLogout={handleLogout}
            handleLogin={handleLogin}
          />
        )}
        {activeTab === 'mcp' && <MCPSettings />}
        {activeTab === 'prompts' && (
          <PromptSettings
            globalPrompts={globalPrompts}
            editingPrompt={editingPrompt}
            isAddingNewPrompt={isAddingNewPrompt}
            newPromptLabel={newPromptLabel}
            newPromptValue={newPromptValue}
            setNewPromptLabel={setNewPromptLabel}
            setNewPromptValue={setNewPromptValue}
            handleSavePrompt={handleSavePrompt}
            handleCancelEdit={handleCancelEdit}
            handleEditPrompt={handleEditPrompt}
            handleDeletePrompt={handleDeletePrompt}
            handleAddNewPrompt={handleAddNewPrompt}
            handleMoveUp={handleMoveUp}
            handleMoveDown={handleMoveDown}
          />
        )}
      </div>
    </div>
  );
};

export default SettingsPage;
