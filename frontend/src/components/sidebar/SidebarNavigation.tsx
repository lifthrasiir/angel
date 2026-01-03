import React from 'react';
import { FaCog, FaSearch } from 'react-icons/fa';

interface SidebarNavigationProps {
  onNavigate: (path: string) => void;
}

export const SidebarNavigation: React.FC<SidebarNavigationProps> = ({ onNavigate }) => {
  return (
    <>
      <hr
        style={{
          width: '100%',
          height: '1px',
          border: '0',
          backgroundColor: '#ccc',
        }}
      />
      <button
        onClick={() => onNavigate('/search')}
        style={{
          width: '100%',
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          cursor: 'pointer',
          color: 'black',
          textDecoration: 'none',
          textAlign: 'left',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'flex-start',
          border: '0',
          padding: '5px',
          backgroundColor: 'transparent',
          minHeight: 'var(--touch-target-size)',
        }}
        aria-label="Go to Search"
      >
        <FaSearch style={{ marginRight: '5px' }} />
        Search
      </button>
      <button
        onClick={() => onNavigate('/settings')}
        style={{
          width: '100%',
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          cursor: 'pointer',
          color: 'black',
          textDecoration: 'none',
          textAlign: 'left',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'flex-start',
          border: '0',
          padding: '5px',
          backgroundColor: 'transparent',
          minHeight: 'var(--touch-target-size)',
        }}
        aria-label="Go to Settings"
      >
        <FaCog style={{ marginRight: '5px' }} />
        Settings
      </button>
    </>
  );
};

export default SidebarNavigation;
