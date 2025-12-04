import type React from 'react';
import { useEffect, useState } from 'react';
import { apiFetch } from '../api/apiClient';
import { useNavigate } from 'react-router-dom';
import { useAtom, useSetAtom } from 'jotai';
import MCPSettings from '../components/MCPSettings'; // Import the new component
import OpenAISettings from '../components/OpenAISettings'; // Import OpenAI settings component
import GeminiAPISettings from '../components/GeminiAPISettings'; // Import Gemini API settings component
import SystemPromptEditor, { PredefinedPrompt } from '../components/SystemPromptEditor'; // Import SystemPromptEditor and PredefinedPrompt type

import { globalPromptsAtom, hasConnectedAccountsAtom, hasApiKeysAtom, isAuthenticatedAtom } from '../atoms/chatAtoms';

interface Account {
  id: number;
  email: string;
  createdAt: string;
  updatedAt: string;
}

const SettingsPage: React.FC = () => {
  const [activeTab, setActiveTab] = useState('auth');
  const [globalPrompts, setGlobalPrompts] = useAtom(globalPromptsAtom); // Use setGlobalPrompts
  const setHasConnectedAccounts = useSetAtom(hasConnectedAccountsAtom);
  const setHasApiKeys = useSetAtom(hasApiKeysAtom);
  const setIsAuthenticated = useSetAtom(isAuthenticatedAtom);
  const navigate = useNavigate();

  // State for managing global prompts
  const [editingPrompt, setEditingPrompt] = useState<PredefinedPrompt | null>(null);
  const [newPromptLabel, setNewPromptLabel] = useState('');
  const [newPromptValue, setNewPromptValue] = useState('');
  const [isAddingNewPrompt, setIsAddingNewPrompt] = useState(false);

  // State for managing accounts
  const [accounts, setAccounts] = useState<Account[]>([]);

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

  const fetchAccounts = async () => {
    try {
      const response = await apiFetch('/api/accounts');
      if (response.ok) {
        const data: Account[] = await response.json();
        setAccounts(data);
        setHasConnectedAccounts(data.length > 0);
      } else {
        console.error('Failed to fetch accounts:', response.status, response.statusText);
        setHasConnectedAccounts(false);
      }
    } catch (error) {
      console.error('Error fetching accounts:', error);
      setHasConnectedAccounts(false);
    }
  };

  const checkApiKeys = async () => {
    try {
      const [openaiResponse, geminiResponse] = await Promise.all([
        apiFetch('/api/openai-configs'),
        apiFetch('/api/gemini-api-configs'),
      ]);

      let hasAnyApiKeys = false;

      if (openaiResponse.ok) {
        const openaiConfigs = await openaiResponse.json();
        hasAnyApiKeys = hasAnyApiKeys || openaiConfigs.some((config: any) => config.enabled);
      }

      if (geminiResponse.ok) {
        const geminiConfigs = await geminiResponse.json();
        hasAnyApiKeys = hasAnyApiKeys || geminiConfigs.some((config: any) => config.enabled);
      }

      setHasApiKeys(hasAnyApiKeys);
    } catch (error) {
      console.error('Error fetching API keys:', error);
      setHasApiKeys(false);
    }
  };

  const updateAuthenticationStatus = async () => {
    await checkApiKeys();
    // Note: fetchAccounts is already called separately in useEffect
  };

  useEffect(() => {
    document.title = 'Angel: Settings';

    const initializeAuth = async () => {
      await fetchGlobalPrompts();
      await fetchAccounts();
      await checkApiKeys();
    };

    initializeAuth();
  }, []);

  // Update authentication status whenever accounts or API keys change
  useEffect(() => {
    const [hasAccounts, hasApiKeys] = [accounts.length > 0, false];

    // We need to check API keys state separately since it's managed by atom
    // This is a simplified check - in practice, we'd rely on the atom state
    setIsAuthenticated(hasAccounts || hasApiKeys);
  }, [accounts, setIsAuthenticated]);

  const handleLogoutAccount = async (accountId: number, email: string) => {
    if (!window.confirm(`Are you sure you want to logout the account ${email}?`)) {
      return;
    }

    try {
      const response = await apiFetch('/api/logout', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ id: accountId }),
      });
      if (response.ok) {
        // Refresh accounts list and update connected accounts status
        await fetchAccounts();
      } else {
        console.error('Failed to logout account:', response.status, response.statusText);
        alert('Failed to logout account.');
      }
    } catch (error) {
      console.error('Error during account logout:', error);
      alert('Error occurred during account logout.');
    }
  };

  const handleLogin = () => {
    const currentPath = window.location.pathname + window.location.search;
    const redirectToUrl = `/login?redirect_to=${encodeURIComponent(currentPath)}`;
    window.location.href = redirectToUrl;
  };

  // Refresh accounts when the page becomes visible (user returns from login)
  useEffect(() => {
    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible') {
        fetchAccounts();
      }
    };

    document.addEventListener('visibilitychange', handleVisibilityChange);
    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, []);

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
    accounts: Account[];
    handleLogoutAccount: (id: number, email: string) => void;
    handleLogin: () => void;
    updateAuthenticationStatus: () => void;
  }

  const AuthSettings: React.FC<AuthSettingsProps> = ({
    accounts,
    handleLogoutAccount,
    handleLogin,
    updateAuthenticationStatus,
  }) => {
    return (
      <div>
        <h3>Authentication</h3>

        {accounts.length > 0 ? (
          <div>
            <h4>Connected Google Accounts ({accounts.length})</h4>
            <p style={{ fontSize: '14px', color: '#666', marginTop: '5px' }}>
              Accounts are automatically distributed when using the LLM.
            </p>
            <div style={{ marginTop: '10px' }}>
              {accounts.map((account) => (
                <div
                  key={account.id}
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    padding: '8px 12px',
                    margin: '4px 0',
                    backgroundColor: '#f8f9fa',
                    borderRadius: '4px',
                    border: '1px solid #dee2e6',
                  }}
                >
                  <div>
                    <strong>{account.email}</strong>
                  </div>
                  <button
                    onClick={() => handleLogoutAccount(account.id, account.email)}
                    style={{
                      padding: '4px 8px',
                      fontSize: '12px',
                      backgroundColor: '#dc3545',
                      color: 'white',
                      border: 'none',
                      borderRadius: '3px',
                      cursor: 'pointer',
                    }}
                  >
                    Remove
                  </button>
                </div>
              ))}
            </div>
          </div>
        ) : (
          <div>
            <p>No connected accounts.</p>
            <p style={{ fontSize: '14px', color: '#666', marginTop: '5px' }}>
              Add a Google account to start using the application.
            </p>
          </div>
        )}

        <div style={{ marginTop: '20px' }}>
          <button
            onClick={handleLogin}
            style={{
              padding: '8px 16px',
              backgroundColor: '#007bff',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Add New Account
          </button>
        </div>

        <hr style={{ margin: '30px 0', border: 'none', borderTop: '1px solid #eee' }} />

        <GeminiAPISettings onConfigChange={updateAuthenticationStatus} />
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
              onClick={() => setActiveTab('openai')}
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
            accounts={accounts}
            handleLogoutAccount={handleLogoutAccount}
            handleLogin={handleLogin}
            updateAuthenticationStatus={updateAuthenticationStatus}
          />
        )}
        {activeTab === 'mcp' && <MCPSettings />}
        {activeTab === 'openai' && <OpenAISettings onConfigChange={updateAuthenticationStatus} />}
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
