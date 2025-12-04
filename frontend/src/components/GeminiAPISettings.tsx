import type React from 'react';
import { useState, useEffect } from 'react';
import { apiFetch } from '../api/apiClient';

interface GeminiAPIConfig {
  id: string;
  name: string;
  api_key: string;
  enabled: boolean;
  last_used_by_model?: Record<string, string>;
  created_at?: string;
}

interface GeminiAPISettingsProps {
  onConfigChange?: () => void;
}

const GeminiAPISettings: React.FC<GeminiAPISettingsProps> = ({ onConfigChange }) => {
  const [configs, setConfigs] = useState<GeminiAPIConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [editingConfig, setEditingConfig] = useState<GeminiAPIConfig | null>(null);
  const [newConfig, setNewConfig] = useState({ name: '', api_key: '' });
  const [isAddingNew, setIsAddingNew] = useState(false);

  const fetchConfigs = async () => {
    try {
      const response = await apiFetch('/api/gemini-api-configs');
      if (response.ok) {
        const data = await response.json();
        setConfigs(data);
      } else {
        console.error('Failed to fetch Gemini API configs:', response.status);
      }
    } catch (error) {
      console.error('Error fetching Gemini API configs:', error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchConfigs();
  }, []);

  const handleSaveConfig = async () => {
    try {
      const configData = {
        name: newConfig.name.trim(),
        api_key: newConfig.api_key.trim(),
        enabled: true,
      };

      if (editingConfig) {
        // Update existing config
        const response = await apiFetch('/api/gemini-api-configs', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify(configData),
        });

        if (response.ok) {
          await fetchConfigs();
          setEditingConfig(null);
          setNewConfig({ name: '', api_key: '' });
          setIsAddingNew(false);
          onConfigChange?.(); // Notify parent of config change
        } else {
          alert('Failed to update Gemini API config');
        }
      } else {
        // Add new config
        const response = await apiFetch('/api/gemini-api-configs', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify(configData),
        });

        if (response.ok) {
          await fetchConfigs();
          setNewConfig({ name: '', api_key: '' });
          setIsAddingNew(false);
          onConfigChange?.(); // Notify parent of config change
        } else {
          alert('Failed to add Gemini API config');
        }
      }
    } catch (error) {
      console.error('Error saving Gemini API config:', error);
      alert('Error saving config');
    }
  };

  const handleDeleteConfig = async (id: string) => {
    if (window.confirm('Are you sure you want to delete this API config?')) {
      try {
        const response = await apiFetch(`/api/gemini-api-configs/${id}`, {
          method: 'DELETE',
        });

        if (response.ok) {
          await fetchConfigs();
          onConfigChange?.(); // Notify parent of config change
        } else {
          alert('Failed to delete Gemini API config');
        }
      } catch (error) {
        console.error('Error deleting Gemini API config:', error);
        alert('Error deleting config');
      }
    }
  };

  const handleToggleEnabled = async (id: string, enabled: boolean) => {
    try {
      const config = configs.find((c) => c.id === id);
      if (!config) return;

      const response = await apiFetch('/api/gemini-api-configs', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          ...config,
          enabled,
        }),
      });

      if (response.ok) {
        await fetchConfigs();
      } else {
        alert('Failed to update Gemini API config');
      }
    } catch (error) {
      console.error('Error updating Gemini API config:', error);
      alert('Error updating config');
    }
  };

  const handleEditConfig = (config: GeminiAPIConfig) => {
    setEditingConfig(config);
    setNewConfig({ name: config.name, api_key: config.api_key });
    setIsAddingNew(true);
  };

  const handleAddNew = () => {
    setEditingConfig(null);
    setNewConfig({ name: '', api_key: '' });
    setIsAddingNew(true);
  };

  const handleCancel = () => {
    setEditingConfig(null);
    setNewConfig({ name: '', api_key: '' });
    setIsAddingNew(false);
  };

  const formatDate = (dateString?: string) => {
    if (!dateString) return 'Never';
    return new Date(dateString).toLocaleString();
  };

  if (loading) {
    return <div>Loading Gemini API configurations...</div>;
  }

  return (
    <div>
      <h4>Gemini API Configuration</h4>
      <p style={{ color: '#666', fontSize: '14px', marginBottom: '20px' }}>
        Configure Gemini API keys for direct API access. When available, the system will use these keys instead of the
        Code Assist API wrapper. Multiple keys are supported and will be used in rotation with automatic rate limit
        handling.
      </p>

      {isAddingNew && (
        <div
          style={{
            border: '1px solid #ddd',
            padding: '15px',
            marginBottom: '20px',
            borderRadius: '5px',
            backgroundColor: '#f9f9f9',
          }}
        >
          <h5>{editingConfig ? 'Edit API Config' : 'Add New API Config'}</h5>
          <div style={{ marginBottom: '10px' }}>
            <label style={{ display: 'block', marginBottom: '5px' }}>Name:</label>
            <input
              type="text"
              value={newConfig.name}
              onChange={(e) => setNewConfig({ ...newConfig, name: e.target.value })}
              placeholder="e.g., Primary Key, Backup Key"
              style={{
                width: '100%',
                padding: '8px',
                border: '1px solid #ccc',
                borderRadius: '3px',
                boxSizing: 'border-box',
              }}
            />
          </div>
          <div style={{ marginBottom: '10px' }}>
            <label style={{ display: 'block', marginBottom: '5px' }}>API Key:</label>
            <input
              type="password"
              value={newConfig.api_key}
              onChange={(e) => setNewConfig({ ...newConfig, api_key: e.target.value })}
              placeholder="Enter your Gemini API key"
              style={{
                width: '100%',
                padding: '8px',
                border: '1px solid #ccc',
                borderRadius: '3px',
                boxSizing: 'border-box',
              }}
            />
          </div>
          <div>
            <button
              onClick={handleSaveConfig}
              disabled={!newConfig.name.trim() || !newConfig.api_key.trim()}
              style={{
                marginRight: '10px',
                padding: '8px 16px',
                backgroundColor: newConfig.name.trim() && newConfig.api_key.trim() ? '#007bff' : '#ccc',
                color: 'white',
                border: 'none',
                borderRadius: '3px',
                cursor: newConfig.name.trim() && newConfig.api_key.trim() ? 'pointer' : 'not-allowed',
              }}
            >
              {editingConfig ? 'Update' : 'Save'}
            </button>
            <button
              onClick={handleCancel}
              style={{
                padding: '8px 16px',
                backgroundColor: '#6c757d',
                color: 'white',
                border: 'none',
                borderRadius: '3px',
                cursor: 'pointer',
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      <div style={{ marginBottom: '20px' }}>
        <button
          onClick={handleAddNew}
          disabled={isAddingNew}
          style={{
            padding: '8px 16px',
            backgroundColor: '#28a745',
            color: 'white',
            border: 'none',
            borderRadius: '3px',
            cursor: isAddingNew ? 'not-allowed' : 'pointer',
          }}
        >
          Add New API Key
        </button>
      </div>

      {configs.length === 0 && !isAddingNew && (
        <div
          style={{
            padding: '20px',
            textAlign: 'center',
            color: '#666',
            backgroundColor: '#f8f9fa',
            border: '1px dashed #dee2e6',
            borderRadius: '5px',
          }}
        >
          <p>No Gemini API keys configured yet.</p>
          <p style={{ fontSize: '14px' }}>
            Add an API key to enable direct Gemini API access with better rate limit handling.
          </p>
        </div>
      )}

      {configs.length > 0 && (
        <div>
          {configs.map((config) => (
            <div
              key={config.id}
              style={{
                border: '1px solid #ddd',
                padding: '15px',
                marginBottom: '10px',
                borderRadius: '5px',
                backgroundColor: config.enabled ? '#fff' : '#f8f9fa',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div>
                  <h5 style={{ margin: '0 0 5px 0' }}>
                    {config.name}
                    {!config.enabled && <span style={{ color: '#dc3545', marginLeft: '10px' }}>Disabled</span>}
                  </h5>
                  <p style={{ margin: '0', fontSize: '12px', color: '#666' }}>
                    API Key: {config.api_key.substring(0, 12)}...{config.api_key.substring(config.api_key.length - 4)}
                  </p>
                  <p style={{ margin: '5px 0 0 0', fontSize: '12px', color: '#666' }}>
                    Created: {formatDate(config.created_at)}
                  </p>
                  {config.last_used_by_model && Object.keys(config.last_used_by_model).length > 0 && (
                    <p style={{ margin: '5px 0 0 0', fontSize: '12px', color: '#666' }}>
                      Last used by: {Object.keys(config.last_used_by_model).join(', ')}
                    </p>
                  )}
                </div>
                <div>
                  <button
                    onClick={() => handleToggleEnabled(config.id, !config.enabled)}
                    style={{
                      marginRight: '10px',
                      padding: '6px 12px',
                      backgroundColor: config.enabled ? '#ffc107' : '#28a745',
                      color: 'white',
                      border: 'none',
                      borderRadius: '3px',
                      cursor: 'pointer',
                      fontSize: '12px',
                    }}
                  >
                    {config.enabled ? 'Disable' : 'Enable'}
                  </button>
                  <button
                    onClick={() => handleEditConfig(config)}
                    style={{
                      marginRight: '10px',
                      padding: '6px 12px',
                      backgroundColor: '#007bff',
                      color: 'white',
                      border: 'none',
                      borderRadius: '3px',
                      cursor: 'pointer',
                      fontSize: '12px',
                    }}
                  >
                    Edit
                  </button>
                  <button
                    onClick={() => handleDeleteConfig(config.id)}
                    style={{
                      padding: '6px 12px',
                      backgroundColor: '#dc3545',
                      color: 'white',
                      border: 'none',
                      borderRadius: '3px',
                      cursor: 'pointer',
                      fontSize: '12px',
                    }}
                  >
                    Delete
                  </button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

export default GeminiAPISettings;
