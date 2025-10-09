import React, { useState, useEffect } from 'react';
import { getDirectoryListing, confirmDirectorySelection } from '../utils/dialogHelpers';

interface DirectoryPickerProps {
  isOpen: boolean;
  onClose: () => void;
  onDirectorySelected: (path: string) => void;
  initialPath?: string;
}

interface DirectoryInfo {
  name: string;
  path: string;
  isParent: boolean;
  isRoot: boolean;
}

interface DirectoryNavigationResponse {
  currentPath: string;
  items: DirectoryInfo[];
  error?: string;
}

export const DirectoryPicker: React.FC<DirectoryPickerProps> = ({
  isOpen,
  onClose,
  onDirectorySelected,
  initialPath,
}) => {
  const [directories, setDirectories] = useState<DirectoryInfo[]>([]);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string>('');
  const [selectedPath, setSelectedPath] = useState<string>('');
  const [pathInput, setPathInput] = useState<string>('');

  // Load directory listing
  const loadDirectory = async (path: string) => {
    setLoading(true);
    setError('');

    try {
      const data: DirectoryNavigationResponse = await getDirectoryListing(path);

      if (data.error) {
        setError(data.error);
        setDirectories([]); // Clear directories on error
        // Keep the attempted path in input for correction
        setPathInput(path);
      } else {
        setPathInput(data.currentPath);
        setDirectories(data.items);
      }
    } catch (err: any) {
      setError(`Failed to load directory: ${err.message}`);
      setDirectories([]); // Clear directories on error
      // Keep the attempted path in input for correction
      setPathInput(path);
    } finally {
      setLoading(false);
    }
  };

  // Handle directory selection
  const handleDirectoryClick = (dir: DirectoryInfo) => {
    if (dir.isParent) {
      // For parent directory, navigate directly instead of selecting
      loadDirectory(dir.path);
    } else {
      setSelectedPath(dir.path);
    }
  };

  // Handle directory navigation
  const handleDirectoryNavigate = (dir: DirectoryInfo) => {
    loadDirectory(dir.path);
  };

  // Handle path input change
  const handlePathInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setPathInput(e.target.value);
  };

  // Handle path input submit (Enter key)
  const handlePathInputSubmit = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      const input = pathInput.trim();
      if (input === '') {
        // Don't allow empty string input directly
        return;
      }
      loadDirectory(input);
    }
  };

  // Handle navigate button click
  const handleNavigateClick = () => {
    const input = pathInput.trim();
    if (input === '') {
      // Don't allow empty string input directly
      return;
    }
    loadDirectory(input);
  };

  // Handle select button click
  const handleSelect = async () => {
    // If no path is selected, use the current path (unless it's virtual root)
    const pathToSelect = selectedPath || pathInput;

    // Don't allow selecting virtual root (empty string)
    if (!pathToSelect) return;

    try {
      const data = await confirmDirectorySelection(pathToSelect);

      if (data.result === 'success' && data.selectedPath) {
        onDirectorySelected(data.selectedPath);
        onClose();
      } else {
        setError(data.error || 'Failed to select directory');
      }
    } catch (err: any) {
      setError(`Failed to select directory: ${err.message}`);
    }
  };

  // Initialize with initial path or current working directory
  useEffect(() => {
    if (isOpen) {
      setSelectedPath('');
      setError('');
      loadDirectory(initialPath || '.');
    }
  }, [isOpen, initialPath]);

  // Determine if select button should be enabled
  const canSelect = pathInput !== ''; // Enable if not in virtual root (empty string)

  if (!isOpen) return null;

  return (
    <div
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        width: '100%',
        height: '100%',
        backgroundColor: 'rgba(0, 0, 0, 0.5)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 9999,
        userSelect: 'none',
      }}
    >
      <div
        style={{
          backgroundColor: 'white',
          borderRadius: '8px',
          boxShadow: '0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -2px rgba(0, 0, 0, 0.05)',
          width: '100%',
          maxWidth: '672px',
          maxHeight: '80vh',
          display: 'flex',
          flexDirection: 'column',
          userSelect: 'none',
        }}
      >
        {/* Header */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '16px',
            borderBottom: '1px solid #e5e7eb',
          }}
        >
          <h2 style={{ fontSize: '18px', fontWeight: 600 }}>Select Directory</h2>
          <button
            onClick={onClose}
            style={{
              color: '#6b7280',
              fontSize: '20px',
              fontWeight: 'bold',
              background: 'none',
              border: 'none',
              cursor: 'pointer',
              padding: '4px',
            }}
            onMouseEnter={(e) => (e.currentTarget.style.color = '#374151')}
            onMouseLeave={(e) => (e.currentTarget.style.color = '#6b7280')}
          >
            ×
          </button>
        </div>

        {/* Current Path */}
        <div
          style={{
            padding: '16px',
            borderBottom: '1px solid #e5e7eb',
            backgroundColor: '#f9fafb',
          }}
        >
          <div style={{ fontSize: '14px', color: '#6b7280', marginBottom: '4px' }}>Current Path:</div>
          <div style={{ display: 'flex', gap: '8px' }}>
            <input
              type="text"
              value={pathInput}
              onChange={handlePathInputChange}
              onKeyDown={handlePathInputSubmit}
              placeholder="Enter directory path..."
              style={{
                flex: 1,
                fontSize: '14px',
                fontFamily: 'monospace',
                backgroundColor: 'white',
                padding: '8px',
                borderRadius: '4px',
                border: '1px solid #d1d5db',
                outline: 'none',
              }}
            />
            <button
              onClick={handleNavigateClick}
              style={{
                padding: '8px 16px',
                backgroundColor: '#3b82f6',
                color: 'white',
                border: 'none',
                borderRadius: '4px',
                cursor: 'pointer',
                fontSize: '14px',
              }}
              onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = '#2563eb')}
              onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = '#3b82f6')}
            >
              Navigate
            </button>
          </div>
        </div>

        {/* Error Message */}
        {error && (
          <div
            style={{
              padding: '16px',
              backgroundColor: '#fef2f2',
              borderBottom: '1px solid #e5e7eb',
            }}
          >
            <div style={{ fontSize: '14px', color: '#dc2626' }}>{error}</div>
          </div>
        )}

        {/* Directory List */}
        <div style={{ flex: 1, overflow: 'auto' }}>
          {loading ? (
            <div
              style={{
                padding: '32px',
                textAlign: 'center',
                color: '#6b7280',
              }}
            >
              Loading directories...
            </div>
          ) : !directories || directories.length === 0 ? (
            <div
              style={{
                padding: '32px',
                textAlign: 'center',
                color: '#6b7280',
              }}
            >
              No directories found
            </div>
          ) : directories && directories.length > 0 ? (
            <div style={{ padding: '8px' }}>
              {directories.map((dir, index) => (
                <div
                  key={index}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    padding: '12px',
                    borderRadius: '8px',
                    cursor: 'pointer',
                    backgroundColor: selectedPath === dir.path ? '#eff6ff' : 'transparent',
                    border: selectedPath === dir.path ? '1px solid #3b82f6' : '1px solid transparent',
                  }}
                  onClick={() => handleDirectoryClick(dir)}
                  onDoubleClick={() => handleDirectoryNavigate(dir)}
                  onMouseEnter={(e) => {
                    if (selectedPath !== dir.path) {
                      e.currentTarget.style.backgroundColor = '#f3f4f6';
                    }
                  }}
                  onMouseLeave={(e) => {
                    if (selectedPath !== dir.path) {
                      e.currentTarget.style.backgroundColor = 'transparent';
                    }
                  }}
                >
                  <div style={{ flex: 1, display: 'flex', alignItems: 'center' }}>
                    <span style={{ color: '#9ca3af', marginRight: '12px' }}>{dir.isParent ? '↶' : '📁'}</span>
                    <span
                      style={{
                        fontWeight: 500,
                        color: dir.isParent ? '#6b7280' : '#111827',
                        fontStyle: dir.isParent ? 'italic' : 'normal',
                      }}
                    >
                      {dir.name}
                    </span>
                  </div>
                  {!dir.isParent && (
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        handleDirectoryNavigate(dir);
                      }}
                      style={{
                        padding: '4px 8px',
                        fontSize: '12px',
                        backgroundColor: '#3b82f6',
                        color: 'white',
                        border: 'none',
                        borderRadius: '4px',
                        cursor: 'pointer',
                        opacity: 0,
                        transition: 'opacity 0.2s',
                      }}
                      onMouseEnter={(e) => (e.currentTarget.style.opacity = '1')}
                      onMouseLeave={(e) => (e.currentTarget.style.opacity = '0')}
                    >
                      Open
                    </button>
                  )}
                </div>
              ))}
            </div>
          ) : (
            <div
              style={{
                padding: '32px',
                textAlign: 'center',
                color: '#6b7280',
              }}
            >
              No directories found
            </div>
          )}
        </div>

        {/* Footer */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '16px',
            borderTop: '1px solid #e5e7eb',
            backgroundColor: '#f9fafb',
          }}
        >
          <div style={{ fontSize: '14px', color: '#6b7280' }}>
            {(selectedPath || pathInput) && (
              <span>
                Selected: <span style={{ fontFamily: 'monospace' }}>{selectedPath || pathInput}</span>
              </span>
            )}
          </div>
          <div style={{ display: 'flex', gap: '8px' }}>
            <button
              onClick={onClose}
              style={{
                padding: '8px 16px',
                color: '#4b5563',
                backgroundColor: '#e5e7eb',
                border: 'none',
                borderRadius: '4px',
                cursor: 'pointer',
                fontSize: '14px',
              }}
              onMouseEnter={(e) => (e.currentTarget.style.backgroundColor = '#d1d5db')}
              onMouseLeave={(e) => (e.currentTarget.style.backgroundColor = '#e5e7eb')}
            >
              Cancel
            </button>
            <button
              onClick={handleSelect}
              disabled={!canSelect || loading}
              style={{
                padding: '8px 16px',
                backgroundColor: !canSelect || loading ? '#d1d5db' : '#3b82f6',
                color: 'white',
                border: 'none',
                borderRadius: '4px',
                cursor: !canSelect || loading ? 'not-allowed' : 'pointer',
                fontSize: '14px',
              }}
            >
              Select
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
