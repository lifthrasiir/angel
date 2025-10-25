import React, { useState, useRef, useEffect } from 'react';
import { FaEllipsisV, FaEdit, FaTrash } from 'react-icons/fa';

interface SessionMenuProps {
  sessionId: string;
  sessionName: string;
  onRename: (sessionId: string) => void;
  onDelete: (sessionId: string) => void;
  isMobile?: boolean;
}

const SessionMenu: React.FC<SessionMenuProps> = ({ sessionId, sessionName, onRename, onDelete, isMobile = false }) => {
  const [isOpen, setIsOpen] = useState(false);
  const [menuPosition, setMenuPosition] = useState({ top: 0, left: 0 });
  const [menuWidth, setMenuWidth] = useState(150);
  const menuRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };

    const handleScroll = () => {
      if (isOpen) {
        calculateMenuPosition();
      }
    };

    const handleResize = () => {
      if (isOpen) {
        calculateMenuPosition();
      }
    };

    // Find scrollable parents
    const findScrollableParents = (element: HTMLElement | null): HTMLElement[] => {
      const scrollableParents: HTMLElement[] = [];
      let current = element;

      while (current && current !== document.body) {
        if (current.scrollHeight > current.clientHeight) {
          scrollableParents.push(current);
        }
        current = current.parentElement;
      }

      return scrollableParents;
    };

    document.addEventListener('mousedown', handleClickOutside);
    window.addEventListener('scroll', handleScroll);
    window.addEventListener('resize', handleResize);

    // Add scroll listeners to all scrollable parents
    const scrollableParents = findScrollableParents(triggerRef.current);
    scrollableParents.forEach((parent) => {
      parent.addEventListener('scroll', handleScroll);
    });

    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      window.removeEventListener('scroll', handleScroll);
      window.removeEventListener('resize', handleResize);
      scrollableParents.forEach((parent) => {
        parent.removeEventListener('scroll', handleScroll);
      });
    };
  }, [isOpen]);

  const handleRename = () => {
    onRename(sessionId);
    setIsOpen(false);
  };

  const handleDelete = () => {
    onDelete(sessionId);
    setIsOpen(false);
  };

  // Calculate menu position when opening
  const calculateMenuPosition = () => {
    if (!triggerRef.current) return;

    const rect = triggerRef.current.getBoundingClientRect();
    let menuWidth = 150; // Default width from CSS
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;

    let left = rect.right + 8; // 8px margin to the right
    let top = rect.top;

    // Adjust if menu would go beyond right edge
    if (left + menuWidth > viewportWidth) {
      left = rect.left - menuWidth - 8; // Show to the left instead
    }

    // Adjust if menu would go beyond left edge
    if (left < 8) {
      left = 8;
    }

    // For mobile, show below the button, use wider menu and better alignment
    if (isMobile) {
      const mobileMenuWidth = Math.min(200, viewportWidth - 16); // Wider menu on mobile
      left = rect.left; // Align with left edge of the three-dot button
      // Ensure menu doesn't go beyond right edge on mobile
      if (left + mobileMenuWidth > viewportWidth - 8) {
        left = Math.max(8, viewportWidth - mobileMenuWidth - 8);
      }
      top = rect.bottom + 4;
      // Update menu width for mobile
      menuWidth = mobileMenuWidth;
    }

    // Ensure menu doesn't go below viewport
    const menuHeight = 100; // Estimated height
    if (top + menuHeight > viewportHeight) {
      top = rect.top - menuHeight - 4;
    }

    setMenuPosition({ top, left });
    setMenuWidth(menuWidth);
  };

  const handleToggleMenu = () => {
    if (!isOpen) {
      calculateMenuPosition();
    }
    setIsOpen(!isOpen);
  };

  return (
    <div
      ref={menuRef}
      style={{
        position: 'relative',
        display: 'inline-block',
      }}
    >
      {/* Three-dot menu trigger button */}
      <button
        ref={triggerRef}
        onClick={handleToggleMenu}
        className="session-menu-trigger"
        title={`Session options: ${sessionName}`}
        aria-label={`Session options: ${sessionName}`}
      >
        <FaEllipsisV size={16} />
      </button>

      {/* Dropdown menu */}
      {isOpen && (
        <div
          className={`session-menu ${isMobile ? 'session-menu-mobile' : 'session-menu-desktop'}`}
          style={{
            position: 'fixed',
            top: `${menuPosition.top}px`,
            left: `${menuPosition.left}px`,
            zIndex: 1000,
            width: `${menuWidth}px`,
            background: 'white',
            border: '1px solid #ddd',
            borderRadius: '6px',
            boxShadow: '0 4px 12px rgba(0, 0, 0, 0.15)',
            padding: '4px 0',
          }}
        >
          <button
            onClick={handleRename}
            className="session-menu-item"
            style={{
              color: '#333',
            }}
          >
            <FaEdit size={14} />
            Rename
          </button>
          <button
            onClick={handleDelete}
            className="session-menu-item"
            style={{
              color: '#dc3545',
            }}
          >
            <FaTrash size={14} />
            Delete
          </button>
        </div>
      )}
    </div>
  );
};

export default SessionMenu;
