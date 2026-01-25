import React from 'react';
import { FaEllipsisV, FaEdit, FaTrash, FaChevronRight, FaPlus, FaArchive } from 'react-icons/fa';
import { apiFetch } from '../../api/apiClient';
import type { Workspace, ChatMessage } from '../../types/chat';
import { useCommandProcessor } from '../../hooks/useCommandProcessor';
import Dropdown, { DropdownItem } from '../Dropdown';

interface SessionMenuProps {
  sessionId: string;
  sessionName: string;
  onRename: (sessionId: string) => void;
  onDelete: (sessionId: string) => void;
  isMobile?: boolean;
  currentWorkspaceId: string;
  workspaces: Workspace[];
  onSessionMoved?: () => void;
  isCurrentSession?: boolean;
  onNavigateToWorkspace?: (workspaceId: string) => void;
  onMenuToggle?: (sessionId: string, isOpen: boolean) => void;
  messages?: ChatMessage[]; // Only provided for current session
  archived?: boolean; // Whether the session is archived
}

const SessionMenu: React.FC<SessionMenuProps> = ({
  sessionId,
  sessionName,
  onRename,
  onDelete,
  isMobile = false,
  currentWorkspaceId,
  workspaces,
  onSessionMoved,
  isCurrentSession = false,
  onNavigateToWorkspace,
  onMenuToggle,
  messages,
  archived = false,
}) => {
  const { runNewMessageCommand } = useCommandProcessor(sessionId);

  const handleRename = () => {
    onRename(sessionId);
    if (onMenuToggle) {
      onMenuToggle(sessionId, false);
    }
  };

  const handleDelete = () => {
    onDelete(sessionId);
    if (onMenuToggle) {
      onMenuToggle(sessionId, false);
    }
  };

  const handleArchiveToggle = async () => {
    try {
      const newArchiveState = !archived;
      const response = await apiFetch(`/api/chat/${sessionId}/archive`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ archive: newArchiveState }),
      });

      if (response.ok) {
        // Close the menu
        if (onMenuToggle) {
          onMenuToggle(sessionId, false);
        }

        // Refresh the page to update the session list
        window.location.reload();
      } else {
        console.error('Failed to toggle archive state:', response.statusText);
      }
    } catch (error) {
      console.error('Error toggling archive state:', error);
    }
  };

  const handleMoveToWorkspace = async (workspaceId: string) => {
    try {
      const response = await apiFetch(`/api/chat/${sessionId}/workspace`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ workspaceId }),
      });

      if (response.ok) {
        // Notify parent that menu closed
        if (onMenuToggle) {
          onMenuToggle(sessionId, false);
        }

        // If this is the current session, navigate to the new workspace
        if (isCurrentSession && onNavigateToWorkspace) {
          onNavigateToWorkspace(workspaceId);
        }

        if (onSessionMoved) {
          onSessionMoved();
        }
      } else {
        console.error('Failed to move session:', response.statusText);
      }
    } catch (error) {
      console.error('Error moving session:', error);
    }
  };

  // Factory function to create handlers for new user/model messages
  const makeHandleNewMessage = (commandType: 'new-user-message' | 'new-model-message') => async () => {
    // Close the menu
    if (onMenuToggle) {
      onMenuToggle(sessionId, false);
    }
    // Use the command processor to run the command
    await runNewMessageCommand(commandType);
  };

  // Only show append options when messages are provided (not undefined)
  let showNewModelMessage = false;
  let showNewUserMessage = false;
  if (isCurrentSession && messages !== undefined) {
    const lastMessageType = messages.length > 0 ? messages[messages.length - 1].type : null;
    showNewUserMessage = lastMessageType !== 'user';
    showNewModelMessage = lastMessageType !== 'model';
  }

  // Create workspace submenu items
  const workspaceSubmenuItems: DropdownItem[] = [
    // Anonymous workspace option
    ...(currentWorkspaceId !== ''
      ? [
          {
            id: 'no-workspace',
            label: '(No Workspace)',
            onClick: () => handleMoveToWorkspace(''),
          } as DropdownItem,
        ]
      : []),
    // Other workspaces
    ...workspaces
      .filter((workspace) => workspace.id !== currentWorkspaceId)
      .map((workspace) => ({
        id: workspace.id,
        label: workspace.name,
        onClick: () => handleMoveToWorkspace(workspace.id),
      })),
  ];

  // Add "no workspaces" message if needed
  if (workspaceSubmenuItems.length === 0 && currentWorkspaceId !== '') {
    workspaceSubmenuItems.push({
      id: 'no-other-workspaces',
      label: 'No other workspaces',
      disabled: true,
    });
  }

  // Main menu items
  const menuItems: DropdownItem[] = [
    // Append new user message (only for current session, not when last message is user)
    ...(showNewUserMessage
      ? [
          {
            id: 'new-user-message',
            label: 'Append new user message',
            icon: <FaPlus size={14} />,
            onClick: makeHandleNewMessage('new-user-message'),
          } as DropdownItem,
        ]
      : []),
    // Append new model message (only for current session, not when last message is model)
    ...(showNewModelMessage
      ? [
          {
            id: 'new-model-message',
            label: 'Append new model message',
            icon: <FaPlus size={14} />,
            onClick: makeHandleNewMessage('new-model-message'),
          } as DropdownItem,
        ]
      : []),
    // Divider (only show when we have append options)
    ...(showNewUserMessage || showNewModelMessage ? ['-' as const] : []),
    {
      id: 'rename',
      label: 'Rename',
      icon: <FaEdit size={14} />,
      onClick: handleRename,
    },
    {
      id: 'move-to',
      label: 'Move to...',
      icon: <FaChevronRight size={14} />,
      submenu: workspaceSubmenuItems,
    },
    {
      id: 'archive',
      label: archived ? 'Unarchive' : 'Archive',
      icon: <FaArchive size={14} />,
      onClick: handleArchiveToggle,
    },
    {
      id: 'delete',
      label: 'Delete',
      icon: <FaTrash size={14} />,
      onClick: handleDelete,
      danger: true,
    },
  ];

  return (
    <Dropdown
      trigger={
        <button
          className="session-menu-trigger"
          title={`Session options: ${sessionName}`}
          aria-label={`Session options: ${sessionName}`}
        >
          <FaEllipsisV size={16} />
        </button>
      }
      items={menuItems}
      isMobile={isMobile}
      menuWidth={isMobile ? 200 : 150}
      onOpen={() => onMenuToggle?.(sessionId, true)}
      onClose={() => onMenuToggle?.(sessionId, false)}
    />
  );
};

export default SessionMenu;
