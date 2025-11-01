import React from 'react';
import { FaEllipsisV, FaEdit, FaTrash, FaChevronRight } from 'react-icons/fa';
import { apiFetch } from '../api/apiClient';
import type { Workspace } from '../types/chat';
import Dropdown, { DropdownItem } from './Dropdown';

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
}) => {
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
