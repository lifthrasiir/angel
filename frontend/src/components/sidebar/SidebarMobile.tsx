import React from 'react';

interface SidebarMobileProps {
  isOpen: boolean;
  onOverlayClick: () => void;
  children: React.ReactNode;
}

export const SidebarMobile: React.FC<SidebarMobileProps> = ({ isOpen, onOverlayClick, children }) => {
  return (
    <>
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
