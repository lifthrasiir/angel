import React from 'react';
import { useNavigate } from 'react-router-dom';
import { FaEllipsisH, FaCut, FaCopy, FaSync } from 'react-icons/fa';
import { useSetAtom } from 'jotai';
import type { ChatMessage, Session } from '../../types/chat';
import Dropdown, { DropdownItem } from '../Dropdown';
import { extractSession, type ExtractResponse } from '../../api/apiClient';
import { addSessionAtom, resetChatSessionStateAtom } from '../../atoms/chatAtoms';
import { useCopyToClipboard } from '../../hooks/useCopyToClipboard';

export interface MessageMenuProps {
  message: ChatMessage;
  sessionId: string;
  isMobile?: boolean;
  className?: string;
  onUpdateClick?: () => void;
}

const MessageMenu: React.FC<MessageMenuProps> = ({
  message,
  sessionId,
  isMobile = false,
  className = '',
  onUpdateClick,
}) => {
  const navigate = useNavigate();
  const addSession = useSetAtom(addSessionAtom);
  const resetChatSessionState = useSetAtom(resetChatSessionStateAtom);
  const { copyToClipboard } = useCopyToClipboard();

  const handleExtract = async () => {
    try {
      const result: ExtractResponse = await extractSession(sessionId, message.id);

      // Add the new session to the sessions list
      const newSession: Session = {
        id: result.sessionId,
        name: result.sessionName,
        last_updated_at: new Date().toISOString(),
      };

      // Reset chat session state and add new session
      resetChatSessionState();
      addSession(newSession);

      // Navigate to the new session
      navigate(result.link);
    } catch (error) {
      console.error('Failed to extract session:', error);
      // You could show a toast notification here
    }
  };

  const handleCopy = async () => {
    const text = message.parts?.[0]?.text || '';
    await copyToClipboard(text);
  };

  // Menu items with copy and extract actions
  const menuItems: DropdownItem[] = [
    ...(onUpdateClick
      ? [
          {
            id: 'update',
            label: 'Update in place',
            icon: <FaSync size={14} />,
            onClick: onUpdateClick,
          } as DropdownItem,
        ]
      : []),
    {
      id: 'copy',
      label: 'Copy',
      icon: <FaCopy size={14} />,
      onClick: handleCopy,
    },
    {
      id: 'extract',
      label: 'Extract',
      icon: <FaCut size={14} />,
      onClick: handleExtract,
    },
  ];

  return (
    <Dropdown
      trigger={
        <button className={`message-menu-trigger ${className}`} title="Message options" aria-label="Message options">
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
