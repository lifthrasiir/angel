import React from 'react';
import { FaEllipsisH } from 'react-icons/fa';
import type { ChatMessage } from '../types/chat';
import Dropdown, { DropdownItem } from './Dropdown';

export interface MessageMenuProps {
  message: ChatMessage;
  isMobile?: boolean;
  className?: string;
}

const MessageMenu: React.FC<MessageMenuProps> = ({ message, isMobile = false, className = '' }) => {
  const handleCopy = () => {
    // Copy message text to clipboard using execCommand (fallback method)
    const text = message.parts
      .map((part) => part.text || '')
      .join('')
      .trim();

    // Create a temporary textarea element
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.style.position = 'fixed';
    textarea.style.opacity = '0';
    document.body.appendChild(textarea);

    try {
      // Select and copy the text
      textarea.select();
      const successful = document.execCommand('copy');
      if (!successful) {
        console.error('Failed to copy text using execCommand');
      }
    } catch (err) {
      console.error('Failed to copy text: ', err);
    } finally {
      // Remove the temporary textarea
      document.body.removeChild(textarea);
    }
  };

  // Simple menu with just copy action
  const menuItems: DropdownItem[] = [
    {
      id: 'copy',
      label: 'Copy',
      onClick: handleCopy,
    },
  ];

  return (
    <Dropdown
      trigger={
        <button className={`session-menu-trigger ${className}`} title="Message options" aria-label="Message options">
          <FaEllipsisH size={16} />
        </button>
      }
      items={menuItems}
      isMobile={isMobile}
      menuWidth={120}
      position="below"
    />
  );
};

export default MessageMenu;
