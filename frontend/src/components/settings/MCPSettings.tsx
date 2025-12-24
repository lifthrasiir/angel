import type React from 'react';
import { useEffect, useState } from 'react';
import { apiFetch } from '../../api/apiClient';

interface MCPConfig {
  name: string;
  config_json: any; // Assuming it's a JSON object
  enabled: boolean;
  // Frontend-only fields, reflecting live state from backend manager
  is_connected?: boolean;
  available_tools?: string[];
}

const MCPSettings: React.FC = () => {
  const [configs, setConfigs] = useState<MCPConfig[]>([]);
  const [newConfig, setNewConfig] = useState<Partial<MCPConfig>>({
    name: '',
    config_json: { type: 'sse', endpoint: '' },
    enabled: true,
  });

  useEffect(() => {
    fetchConfigs();
  }, []);

  const fetchConfigs = async () => {
    try {
      const response = await apiFetch('/api/mcp/configs');
      if (response.ok) {
        const data = await response.json();
        setConfigs(data || []); // Ensure data is not null
      } else {
        console.error('Failed to fetch MCP configs');
      }
    } catch (error) {
      console.error('Error fetching MCP configs:', error);
    }
  };

  const handleSave = async (configToSave: MCPConfig | Partial<MCPConfig>) => {
    // Ensure config_json is a string before sending
    const payload = {
      ...configToSave,
      config_json: JSON.stringify(configToSave.config_json),
    };

    try {
      const response = await apiFetch('/api/mcp/configs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (response.ok) {
        fetchConfigs(); // Refresh the list
        setNewConfig({
          name: '',
          config_json: { type: 'sse', endpoint: '' },
          enabled: true,
        }); // Reset form
      } else {
        console.error('Failed to save MCP config');
      }
    } catch (error) {
      console.error('Error saving MCP config:', error);
    }
  };

  const handleDelete = async (name: string) => {
    if (window.confirm(`Are you sure you want to delete the MCP config "${name}"?`)) {
      try {
        const response = await apiFetch(`/api/mcp/configs/${name}`, {
          method: 'DELETE',
        });
        if (response.ok) {
          fetchConfigs(); // Refresh the list
        } else {
          console.error('Failed to delete MCP config');
        }
      } catch (error) {
        console.error('Error deleting MCP config:', error);
      }
    }
  };

  return (
    <div>
      <h3>MCP Server Configurations</h3>

      {/* List of existing configs */}
      <div style={{ marginBottom: '20px' }}>
        {configs.map((config) => (
          <div
            key={config.name}
            style={{
              border: '1px solid #ccc',
              borderRadius: '5px',
              padding: '10px',
              marginBottom: '10px',
            }}
          >
            <div
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
              }}
            >
              <strong>{config.name}</strong>
              <div>
                <span
                  style={{
                    color: config.is_connected ? 'green' : 'gray',
                    marginRight: '10px',
                  }}
                >
                  {config.is_connected ? '● Connected' : '○ Disconnected'}
                </span>
                <button onClick={() => handleDelete(config.name)} style={{ color: 'red' }}>
                  Delete
                </button>
              </div>
            </div>
            <p>
              Endpoint:{' '}
              <code>{typeof config.config_json === 'object' ? config.config_json.endpoint : config.config_json}</code>
            </p>
            {config.available_tools && config.available_tools.length > 0 && (
              <div>
                <p>Available Tools:</p>
                <ul style={{ paddingLeft: '20px', marginTop: '5px' }}>
                  {config.available_tools.map((tool) => (
                    <li key={tool}>{tool}</li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Form to add a new config */}
      <div>
        <h4>Add New MCP Server</h4>
        <input
          type="text"
          placeholder="Name (e.g., my-mcp-server)"
          value={newConfig.name || ''}
          onChange={(e) => setNewConfig({ ...newConfig, name: e.target.value })}
          style={{ marginRight: '10px', padding: '5px' }}
        />
        <input
          type="text"
          placeholder="SSE Endpoint URL"
          value={newConfig.config_json?.endpoint || ''}
          onChange={(e) =>
            setNewConfig({
              ...newConfig,
              config_json: {
                ...(newConfig.config_json || {}),
                endpoint: e.target.value,
              },
            })
          }
          style={{ marginRight: '10px', padding: '5px', width: '300px' }}
        />
        <button onClick={() => handleSave(newConfig)}>Add</button>
      </div>
    </div>
  );
};

export default MCPSettings;
