import React, { useState, useRef, useEffect, ReactNode } from 'react';

export interface DropdownItem {
  id: string;
  label: string;
  icon?: ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  danger?: boolean;
  submenu?: DropdownItem[];
}

export interface DropdownProps {
  trigger: ReactNode;
  items: DropdownItem[];
  isMobile?: boolean;
  menuWidth?: number;
  position?: 'left' | 'right' | 'auto' | 'below';
  onClose?: () => void;
  onOpen?: () => void;
  className?: string;
  menuClassName?: string;
  disabled?: boolean;
}

const Dropdown: React.FC<DropdownProps> = ({
  trigger,
  items,
  isMobile = false,
  menuWidth = 150,
  position = 'auto',
  onClose,
  onOpen,
  className = '',
  menuClassName = '',
  disabled = false,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const [menuPosition, setMenuPosition] = useState({ top: 0, left: 0 });
  const [activeSubmenu, setActiveSubmenu] = useState<string | null>(null);
  const [submenuPosition, setSubmenuPosition] = useState({ top: 0, left: 0 });
  const menuRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setIsOpen(false);
        setActiveSubmenu(null);
        if (onClose && isOpen) {
          onClose();
        }
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

    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside);
      window.addEventListener('scroll', handleScroll);
      window.addEventListener('resize', handleResize);

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
    }
  }, [isOpen, onClose]);

  const calculateMenuPosition = () => {
    if (!triggerRef.current) return;

    const rect = triggerRef.current.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;

    let left = rect.right + 8;
    let top = rect.top;

    // For "below" position, show directly below the trigger
    if (position === 'below') {
      left = rect.left;
      top = rect.bottom + 4;

      // First, align left edges
      if (left + menuWidth > viewportWidth - 8) {
        // If it would overflow, align right edges instead
        left = rect.right - menuWidth;
      }

      // Ensure menu doesn't go beyond left edge
      if (left < 8) {
        left = 8;
      }

      // Ensure menu doesn't go below viewport
      const menuHeight = Math.min(300, items.length * 40 + 8);
      if (top + menuHeight > viewportHeight) {
        top = rect.top - menuHeight - 4;
      }
    } else {
      // Adjust position based on preference
      if (position === 'left') {
        left = rect.left - menuWidth - 8;
      } else if (position === 'auto') {
        // Auto-adjust if menu would go beyond right edge
        if (left + menuWidth > viewportWidth) {
          left = rect.left - menuWidth - 8;
        }
      }

      // Ensure menu doesn't go beyond left edge
      if (left < 8) {
        left = 8;
      }

      // For mobile, show below the button
      if (isMobile) {
        const mobileMenuWidth = Math.min(menuWidth, viewportWidth - 16);
        left = rect.left;
        if (left + mobileMenuWidth > viewportWidth - 8) {
          left = Math.max(8, viewportWidth - mobileMenuWidth - 8);
        }
        top = rect.bottom + 4;
      }

      // Ensure menu doesn't go below viewport
      const menuHeight = Math.min(300, items.length * 40 + 8);
      if (top + menuHeight > viewportHeight) {
        top = rect.top - menuHeight - 4;
      }
    }

    setMenuPosition({ top, left });
  };

  const calculateSubmenuPosition = (triggerElement: HTMLElement) => {
    const rect = triggerElement.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const submenuWidth = 200;

    let left = isMobile ? rect.left - submenuWidth - 2 : rect.right + 2;
    let top = rect.top;

    // Adjust if submenu would go beyond right edge
    if (left + submenuWidth > viewportWidth) {
      left = rect.left - submenuWidth - 8;
    }

    // Ensure submenu doesn't go beyond left edge
    if (left < 8) {
      left = 8;
    }

    setSubmenuPosition({ top, left });
  };

  const handleToggleMenu = () => {
    if (disabled) return;

    const newIsOpen = !isOpen;
    if (!isOpen) {
      calculateMenuPosition();
      if (onOpen) onOpen();
    } else {
      if (onClose) onClose();
    }
    setIsOpen(newIsOpen);
    setActiveSubmenu(null);
  };

  const handleItemClick = (item: DropdownItem) => {
    if (item.disabled) return;

    if (item.submenu) {
      // Handle submenu toggle
      const submenuId = item.id;
      if (activeSubmenu === submenuId) {
        setActiveSubmenu(null);
      } else {
        setActiveSubmenu(submenuId);
        // Calculate submenu position
        setTimeout(() => {
          const submenuTrigger = menuRef.current?.querySelector(`[data-submenu-trigger="${submenuId}"]`) as HTMLElement;
          if (submenuTrigger) {
            calculateSubmenuPosition(submenuTrigger);
          }
        }, 0);
      }
    } else {
      // Handle regular item click
      item.onClick?.();
      setIsOpen(false);
      setActiveSubmenu(null);
      if (onClose) onClose();
    }
  };

  const handleSubmenuItemClick = (item: DropdownItem) => {
    if (item.disabled) return;

    item.onClick?.();
    setIsOpen(false);
    setActiveSubmenu(null);
    if (onClose) onClose();
  };

  return (
    <div
      ref={menuRef}
      className={`dropdown ${className}`}
      style={{
        position: 'relative',
        display: 'inline-block',
      }}
    >
      {/* Trigger */}
      <div
        ref={triggerRef}
        onClick={handleToggleMenu}
        className={`dropdown-trigger ${disabled ? 'dropdown-trigger-disabled' : ''}`}
        style={{ cursor: disabled ? 'not-allowed' : 'pointer' }}
      >
        {trigger}
      </div>

      {/* Main Menu */}
      {isOpen && (
        <div
          className={`dropdown-menu ${isMobile ? 'dropdown-menu-mobile' : 'dropdown-menu-desktop'} ${menuClassName}`}
          style={{
            position: 'fixed',
            top: `${menuPosition.top}px`,
            left: `${menuPosition.left}px`,
            zIndex: 1000,
            width: `${isMobile ? Math.min(menuWidth, window.innerWidth - 16) : menuWidth}px`,
            background: 'white',
            border: '1px solid #ddd',
            borderRadius: '6px',
            boxShadow: '0 4px 12px rgba(0, 0, 0, 0.15)',
            padding: '4px 0',
            minWidth: '120px',
            maxWidth: '300px',
          }}
        >
          {items.map((item) => (
            <div key={item.id} style={{ position: 'relative' }}>
              <button
                data-submenu-trigger={item.submenu ? item.id : undefined}
                onClick={() => handleItemClick(item)}
                disabled={item.disabled}
                className={`dropdown-item ${
                  item.disabled ? 'dropdown-item-disabled' : ''
                } ${item.danger ? 'dropdown-item-danger' : ''} ${
                  activeSubmenu === item.id ? 'dropdown-item-active' : ''
                }`}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  width: '100%',
                  padding: '10px 16px',
                  border: 'none',
                  background: 'none',
                  textAlign: 'left',
                  cursor: item.disabled ? 'not-allowed' : 'pointer',
                  fontSize: '14px',
                  gap: '12px',
                  transition: 'background-color 0.2s ease',
                  color: item.danger ? '#dc3545' : '#333',
                  backgroundColor: activeSubmenu === item.id ? '#f0f8ff' : 'transparent',
                  opacity: item.disabled ? 0.5 : 1,
                }}
                onMouseEnter={(e) => {
                  if (!item.disabled && item.submenu) {
                    e.currentTarget.style.backgroundColor = '#f8f9fa';
                  }
                }}
                onMouseLeave={(e) => {
                  if (activeSubmenu !== item.id) {
                    e.currentTarget.style.backgroundColor = 'transparent';
                  }
                }}
              >
                {item.icon && <span style={{ flexShrink: 0 }}>{item.icon}</span>}
                <span style={{ flex: 1 }}>{item.label}</span>
                {item.submenu && <span style={{ fontSize: '10px', opacity: 0.7 }}>▶</span>}
              </button>
            </div>
          ))}

          {items.length === 0 && (
            <div
              style={{
                padding: '8px 16px',
                color: '#999',
                fontSize: '12px',
                textAlign: 'center',
              }}
            >
              No items available
            </div>
          )}
        </div>
      )}

      {/* Submenu */}
      {activeSubmenu && (
        <div
          className={`dropdown-menu dropdown-submenu ${isMobile ? 'dropdown-menu-mobile' : 'dropdown-menu-desktop'}`}
          style={{
            position: 'fixed',
            top: `${submenuPosition.top}px`,
            left: `${submenuPosition.left}px`,
            zIndex: 1001,
            width: '200px',
            background: 'white',
            border: '1px solid #ddd',
            borderRadius: '6px',
            boxShadow: '0 4px 12px rgba(0, 0, 0, 0.15)',
            padding: '4px 0',
          }}
        >
          {items
            .find((item) => item.id === activeSubmenu)
            ?.submenu?.map((subitem) => (
              <button
                key={subitem.id}
                onClick={() => handleSubmenuItemClick(subitem)}
                disabled={subitem.disabled}
                className={`dropdown-item ${
                  subitem.disabled ? 'dropdown-item-disabled' : ''
                } ${subitem.danger ? 'dropdown-item-danger' : ''}`}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  width: '100%',
                  padding: '10px 16px',
                  border: 'none',
                  background: 'none',
                  textAlign: 'left',
                  cursor: subitem.disabled ? 'not-allowed' : 'pointer',
                  fontSize: '14px',
                  gap: '12px',
                  transition: 'background-color 0.2s ease',
                  color: subitem.danger ? '#dc3545' : '#333',
                  opacity: subitem.disabled ? 0.5 : 1,
                }}
                onMouseEnter={(e) => {
                  if (!subitem.disabled) {
                    e.currentTarget.style.backgroundColor = '#f8f9fa';
                  }
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.backgroundColor = 'transparent';
                }}
              >
                {subitem.icon && <span style={{ flexShrink: 0 }}>{subitem.icon}</span>}
                <span style={{ flex: 1 }}>{subitem.label}</span>
              </button>
            ))}
        </div>
      )}
    </div>
  );
};

export default Dropdown;
