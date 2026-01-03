import React from 'react';
import { FaBars, FaTimes } from 'react-icons/fa';

interface SidebarMobileProps {
  isOpen: boolean;
  onToggle: () => void;
  onOverlayClick: () => void;
  children: React.ReactNode;
}

export const SidebarMobile: React.FC<SidebarMobileProps> = ({ isOpen, onToggle, onOverlayClick, children }) => {
  return (
    <>
      {/* Mobile hamburger button */}
      <button
        onClick={onToggle}
        style={{
          position: 'fixed',
          top: '10px',
          left: '10px',
          zIndex: 1001,
          background: '#f0f0f0',
          border: '1px solid #ccc',
          borderRadius: '8px',
          padding: '10px',
          cursor: 'pointer',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          minWidth: '44px',
          minHeight: '44px',
        }}
        aria-label="Toggle menu"
      >
        {isOpen ? <FaTimes /> : <FaBars />}
      </button>

      {/* Mobile overlay */}
      {isOpen && (
        <div
          onClick={onOverlayClick}
          style={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            background: 'rgba(0, 0, 0, 0.5)',
            zIndex: 999,
          }}
        />
      )}

      {/* Sidebar container with mobile positioning */}
      {React.Children.map(children, (child) => {
        if (React.isValidElement(child)) {
          return React.cloneElement(child as any, {
            style: {
              ...(child.props.style as any),
              position: 'fixed',
              top: 0,
              left: isOpen ? 0 : '-100%',
              height: '100vh',
              zIndex: 1000,
              transition: 'left 0.3s ease-in-out',
            },
          });
        }
        return child;
      })}
    </>
  );
};

export default SidebarMobile;
