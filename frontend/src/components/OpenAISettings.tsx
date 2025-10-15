import React, { useEffect, useState } from 'react';
import { apiFetch } from '../api/apiClient';

interface OpenAIConfig {
  id: string;
  name: string;
  endpoint: string;
  api_key: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

interface OpenAIModel {
  id: string;
  object: string;
  created: number;
  owned_by: string;
}

const OpenAISettings: React.FC = () => {
  const [configs, setConfigs] = useState<OpenAIConfig[]>([]);
  const [editingConfig, setEditingConfig] = useState<OpenAIConfig | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [testResults, setTestResults] = useState<{ [key: string]: string }>({});

  // Form state
  const [formData, setFormData] = useState({
    name: '',
    endpoint: '',
    api_key: '',
    enabled: true,
  });

  // Show form state for new configurations
  const [showAddForm, setShowAddForm] = useState(false);

  // Example endpoints for user convenience
  const exampleEndpoints = [
    { label: 'OpenAI API', value: 'https://api.openai.com/v1' },
    { label: 'Ollama (Local)', value: 'http://localhost:11434/v1' },
    { label: 'Azure OpenAI', value: 'https://your-resource.openai.azure.com/openai/deployments/your-deployment' },
    { label: 'Custom Endpoint', value: '' },
  ];

  const fetchConfigs = async () => {
    try {
      const response = await apiFetch('/api/openai-configs');
      if (response.ok) {
        const data: OpenAIConfig[] = await response.json();
        setConfigs(data);
      } else {
        console.error('Failed to fetch OpenAI configs:', response.status);
      }
    } catch (error) {
      console.error('Error fetching OpenAI configs:', error);
    }
  };

  useEffect(() => {
    fetchConfigs();
  }, []);

  const handleSaveConfig = async () => {
    if (!formData.name.trim() || !formData.endpoint.trim()) {
      alert('Name and endpoint are required');
      return;
    }

    setIsLoading(true);
    try {
      const configData = {
        ...formData,
        id: editingConfig?.id || undefined,
      };

      const response = await apiFetch('/api/openai-configs', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(configData),
      });

      if (response.ok) {
        await fetchConfigs();
        resetForm();
        alert(editingConfig ? 'Configuration updated successfully!' : 'Configuration added successfully!');
      } else {
        const errorData = await response.json();
        alert(`Failed to save configuration: ${errorData.message || 'Unknown error'}`);
      }
    } catch (error) {
      console.error('Error saving config:', error);
      alert('Error saving configuration. Please check your connection.');
    } finally {
      setIsLoading(false);
    }
  };

  const handleDeleteConfig = async (config: OpenAIConfig) => {
    if (!window.confirm(`Are you sure you want to delete "${config.name}"?`)) {
      return;
    }

    try {
      const response = await apiFetch(`/api/openai-configs/${config.id}`, {
        method: 'DELETE',
      });

      if (response.ok) {
        await fetchConfigs();
        alert('Configuration deleted successfully!');
      } else {
        alert('Failed to delete configuration');
      }
    } catch (error) {
      console.error('Error deleting config:', error);
      alert('Error deleting configuration');
    }
  };

  const handleEditConfig = (config: OpenAIConfig) => {
    setEditingConfig(config);
    setFormData({
      name: config.name,
      endpoint: config.endpoint,
      api_key: config.api_key,
      enabled: config.enabled,
    });
  };

  const handleTestConnection = async (config: OpenAIConfig) => {
    setTestResults({ ...testResults, [config.id]: 'Testing...' });

    try {
      const response = await apiFetch(`/api/openai-configs/${config.id}/models`);

      if (response.ok) {
        const models: OpenAIModel[] = await response.json();
        setTestResults({
          ...testResults,
          [config.id]: `✅ Connected! Found ${models.length} model(s)`,
        });
      } else {
        setTestResults({
          ...testResults,
          [config.id]: `❌ Failed: ${response.status} ${response.statusText}`,
        });
      }
    } catch (error) {
      console.error('Error testing connection:', error);
      setTestResults({
        ...testResults,
        [config.id]: '❌ Connection failed',
      });
    }
  };

  const handleRefreshModels = async (config: OpenAIConfig) => {
    setTestResults({ ...testResults, [config.id]: 'Refreshing models...' });

    try {
      const response = await apiFetch(`/api/openai-configs/${config.id}/models/refresh`, {
        method: 'POST',
      });

      if (response.ok) {
        const models: OpenAIModel[] = await response.json();
        setTestResults({
          ...testResults,
          [config.id]: `✅ Refreshed! Found ${models.length} model(s)`,
        });
      } else {
        setTestResults({
          ...testResults,
          [config.id]: `❌ Failed to refresh: ${response.status} ${response.statusText}`,
        });
      }
    } catch (error) {
      console.error('Error refreshing models:', error);
      setTestResults({
        ...testResults,
        [config.id]: '❌ Refresh failed',
      });
    }
  };

  const resetForm = () => {
    setFormData({
      name: '',
      endpoint: '',
      api_key: '',
      enabled: true,
    });
    setEditingConfig(null);
    setShowAddForm(false);
  };

  const handleCancelEdit = () => {
    resetForm();
  };

  return (
    <div>
      <h3>OpenAI-Compatible API Configurations</h3>

      {/* Add/Edit Form */}
      {(editingConfig || showAddForm) && (
        <div
          style={{
            border: '1px solid #ddd',
            padding: '15px',
            marginBottom: '20px',
            borderRadius: '8px',
            backgroundColor: '#f9f9f9',
          }}
        >
          <h4>{editingConfig ? 'Edit Configuration' : 'Add New Configuration'}</h4>

          <div style={{ display: 'grid', gap: '10px', marginBottom: '15px' }}>
            <div>
              <label style={{ display: 'block', marginBottom: '5px', fontWeight: 'bold' }}>Name: *</label>
              <input
                type="text"
                value={formData.name}
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                placeholder="e.g., OpenAI GPT-4, Local Ollama"
                style={{ width: '100%', padding: '8px', border: '1px solid #ccc', borderRadius: '4px' }}
              />
            </div>

            <div>
              <label style={{ display: 'block', marginBottom: '5px', fontWeight: 'bold' }}>Endpoint: *</label>
              <input
                type="text"
                value={formData.endpoint}
                onChange={(e) => setFormData({ ...formData, endpoint: e.target.value })}
                placeholder="https://api.openai.com/v1"
                style={{ width: '100%', padding: '8px', border: '1px solid #ccc', borderRadius: '4px' }}
              />
              <div style={{ marginTop: '5px', fontSize: '12px', color: '#666' }}>
                Examples:
                {exampleEndpoints.map((endpoint, index) => (
                  <button
                    key={index}
                    onClick={() => endpoint.value && setFormData({ ...formData, endpoint: endpoint.value })}
                    style={{
                      marginRight: '10px',
                      padding: '2px 6px',
                      fontSize: '11px',
                      backgroundColor: '#e0e0e0',
                      border: '1px solid #ccc',
                      borderRadius: '3px',
                      cursor: 'pointer',
                    }}
                  >
                    {endpoint.label}
                  </button>
                ))}
              </div>
            </div>

            <div>
              <label style={{ display: 'block', marginBottom: '5px', fontWeight: 'bold' }}>API Key:</label>
              <input
                type="password"
                value={formData.api_key}
                onChange={(e) => setFormData({ ...formData, api_key: e.target.value })}
                placeholder="sk-... (optional for local Ollama)"
                style={{ width: '100%', padding: '8px', border: '1px solid #ccc', borderRadius: '4px' }}
              />
            </div>

            <div>
              <label style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                <input
                  type="checkbox"
                  checked={formData.enabled}
                  onChange={(e) => setFormData({ ...formData, enabled: e.target.checked })}
                />
                <span>Enabled</span>
              </label>
            </div>
          </div>

          <div>
            <button
              onClick={handleSaveConfig}
              disabled={isLoading || !formData.name.trim() || !formData.endpoint.trim()}
              style={{
                marginRight: '10px',
                padding: '8px 16px',
                backgroundColor: isLoading ? '#ccc' : '#007bff',
                color: 'white',
                border: 'none',
                borderRadius: '4px',
                cursor: isLoading ? 'not-allowed' : 'pointer',
              }}
            >
              {isLoading ? 'Saving...' : editingConfig ? 'Update' : 'Save'}
            </button>
            <button
              onClick={handleCancelEdit}
              style={{
                padding: '8px 16px',
                backgroundColor: '#6c757d',
                color: 'white',
                border: 'none',
                borderRadius: '4px',
                cursor: 'pointer',
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Add New Button */}
      {!editingConfig && !showAddForm && (
        <button
          onClick={() => {
            setFormData({ name: '', endpoint: '', api_key: '', enabled: true });
            setShowAddForm(true);
          }}
          style={{
            marginBottom: '20px',
            padding: '10px 20px',
            backgroundColor: '#28a745',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: 'pointer',
          }}
        >
          + Add New Configuration
        </button>
      )}

      {/* Configurations List */}
      {configs.length === 0 ? (
        <div
          style={{
            textAlign: 'center',
            padding: '40px',
            border: '1px solid #ddd',
            borderRadius: '8px',
            backgroundColor: '#f8f9fa',
          }}
        >
          <p>No OpenAI-compatible configurations found.</p>
          <p style={{ fontSize: '14px', color: '#666' }}>
            Add a configuration above to start using OpenAI-compatible models.
          </p>
        </div>
      ) : (
        <div>
          {configs.map((config) => (
            <div
              key={config.id}
              style={{
                border: '1px solid #ddd',
                padding: '15px',
                marginBottom: '10px',
                borderRadius: '8px',
                backgroundColor: config.enabled ? '#fff' : '#f8f8f8',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                <div style={{ flex: 1 }}>
                  <h4 style={{ margin: '0 0 10px 0', color: config.enabled ? '#333' : '#999' }}>
                    {config.name} {!config.enabled && '(Disabled)'}
                  </h4>
                  <p style={{ margin: '5px 0', fontSize: '14px', wordBreak: 'break-all' }}>
                    <strong>Endpoint:</strong> {config.endpoint}
                  </p>
                  <p style={{ margin: '5px 0', fontSize: '14px' }}>
                    <strong>API Key:</strong> {config.api_key ? '••••••••••••••••' : 'Not set'}
                  </p>
                  <p style={{ margin: '5px 0', fontSize: '12px', color: '#666' }}>
                    Created: {new Date(config.created_at).toLocaleString()}
                    {config.updated_at !== config.created_at && (
                      <span> • Updated: {new Date(config.updated_at).toLocaleString()}</span>
                    )}
                  </p>
                  {testResults[config.id] && (
                    <p
                      style={{
                        margin: '5px 0',
                        fontSize: '13px',
                        color: testResults[config.id].startsWith('✅') ? '#28a745' : '#dc3545',
                      }}
                    >
                      {testResults[config.id]}
                    </p>
                  )}
                </div>

                <div style={{ display: 'flex', gap: '5px', flexWrap: 'wrap' }}>
                  <button
                    onClick={() => handleTestConnection(config)}
                    style={{
                      padding: '5px 10px',
                      fontSize: '12px',
                      backgroundColor: '#007bff',
                      color: 'white',
                      border: 'none',
                      borderRadius: '3px',
                      cursor: 'pointer',
                    }}
                  >
                    Test
                  </button>
                  <button
                    onClick={() => handleRefreshModels(config)}
                    style={{
                      padding: '5px 10px',
                      fontSize: '12px',
                      backgroundColor: '#17a2b8',
                      color: 'white',
                      border: 'none',
                      borderRadius: '3px',
                      cursor: 'pointer',
                    }}
                  >
                    Refresh
                  </button>
                  <button
                    onClick={() => handleEditConfig(config)}
                    style={{
                      padding: '5px 10px',
                      fontSize: '12px',
                      backgroundColor: '#ffc107',
                      color: '#212529',
                      border: 'none',
                      borderRadius: '3px',
                      cursor: 'pointer',
                    }}
                  >
                    Edit
                  </button>
                  <button
                    onClick={() => handleDeleteConfig(config)}
                    style={{
                      padding: '5px 10px',
                      fontSize: '12px',
                      backgroundColor: '#dc3545',
                      color: 'white',
                      border: 'none',
                      borderRadius: '3px',
                      cursor: 'pointer',
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

      <div
        style={{
          marginTop: '20px',
          padding: '15px',
          backgroundColor: '#e7f3ff',
          borderRadius: '8px',
          fontSize: '14px',
        }}
      >
        <h4 style={{ margin: '0 0 10px 0' }}>💡 Tips:</h4>
        <ul style={{ margin: 0, paddingLeft: '20px' }}>
          <li>Models are automatically discovered and available for chat after configuration</li>
          <li>Models are cached for 24 hours, use "Refresh" to update the model list</li>
        </ul>
      </div>
    </div>
  );
};

export default OpenAISettings;
