import React, { useState, useEffect } from 'react';
import { getDirectoryListing, confirmDirectorySelection } from '../utils/dialogHelpers';
import { Modal } from './Modal';
import './DirectoryPicker.css';

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

  return (
    <Modal isOpen={isOpen} onClose={onClose} className="directory-picker" maxWidthPercentage="95%" maxWidth="672px">
      <Modal.Header onClose={onClose}>
        <h2>Select Directory</h2>
      </Modal.Header>

      <div className="directory-picker-path-section">
        <div className="path-label">Current Path:</div>
        <div className="path-input-row">
          <input
            type="text"
            value={pathInput}
            onChange={handlePathInputChange}
            onKeyDown={handlePathInputSubmit}
            placeholder="Enter directory path..."
            className="path-input"
          />
          <button onClick={handleNavigateClick} className="navigate-button">
            Navigate
          </button>
        </div>
      </div>

      {error && <div className="directory-picker-error">{error}</div>}

      <Modal.Body>
        {loading ? (
          <div className="directory-picker-loading">Loading directories...</div>
        ) : !directories || directories.length === 0 ? (
          <div className="directory-picker-empty">No directories found</div>
        ) : directories && directories.length > 0 ? (
          <div className="directory-list">
            {directories.map((dir, index) => (
              <div
                key={index}
                className={`directory-item ${selectedPath === dir.path ? 'selected' : ''}`}
                onClick={() => handleDirectoryClick(dir)}
                onDoubleClick={() => handleDirectoryNavigate(dir)}
              >
                <div className="directory-item-content">
                  <span className={`directory-icon ${dir.isParent ? 'parent' : ''}`}>{dir.isParent ? '↶' : '📁'}</span>
                  <span className={`directory-name ${dir.isParent ? 'parent' : ''}`}>{dir.name}</span>
                </div>
                {!dir.isParent && (
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      handleDirectoryNavigate(dir);
                    }}
                    className="open-button"
                  >
                    Open
                  </button>
                )}
              </div>
            ))}
          </div>
        ) : (
          <div className="directory-picker-empty">No directories found</div>
        )}
      </Modal.Body>

      <Modal.Footer>
        <div className="selected-path-display">
          {(selectedPath || pathInput) && (
            <span>
              Selected: <span className="path-text">{selectedPath || pathInput}</span>
            </span>
          )}
        </div>
        <div className="footer-buttons">
          <button onClick={onClose} className="cancel-button">
            Cancel
          </button>
          <button onClick={handleSelect} disabled={!canSelect || loading} className="select-button">
            Select
          </button>
        </div>
      </Modal.Footer>
    </Modal>
  );
};
