import type { URLPath } from '../types/sessionFSM';

// Parse the current URL path to determine session type and identifiers
export const parseURLPath = (pathname: string): URLPath => {
  // Remove leading slash and split by '/'
  const segments = pathname.replace(/^\//, '').split('/').filter(Boolean);

  // Pattern: /new -> new global session
  if (segments.length === 1 && segments[0] === 'new') {
    return { type: 'new_session', workspaceId: '', isTemporary: false };
  }

  // Pattern: /temp -> new global temporary session
  if (segments.length === 1 && segments[0] === 'temp') {
    return { type: 'new_session', workspaceId: '', isTemporary: true };
  }

  // Pattern: /w/:workspaceId/new -> new workspace session
  if (segments.length === 3 && segments[0] === 'w' && segments[2] === 'new' && segments[1]) {
    return { type: 'new_session', workspaceId: segments[1], isTemporary: false };
  }

  // Pattern: /w/:workspaceId/temp -> new workspace temporary session
  if (segments.length === 3 && segments[0] === 'w' && segments[2] === 'temp' && segments[1]) {
    return { type: 'new_session', workspaceId: segments[1], isTemporary: true };
  }

  // Pattern: /:sessionId -> existing session
  if (segments.length === 1 && segments[0]) {
    return { type: 'existing_session', sessionId: segments[0] };
  }

  // Default fallback - treat as new global session
  return { type: 'new_session', workspaceId: '', isTemporary: false };
};

// Convert URLPath back to a URL string (useful for navigation)
export const urlPathToURL = (urlPath: URLPath): string => {
  switch (urlPath.type) {
    case 'new_session':
      return (urlPath.workspaceId ? `/w/${urlPath.workspaceId}` : '') + (urlPath.isTemporary ? '/temp' : '/new');

    case 'existing_session':
      return `/${urlPath.sessionId}`;

    default:
      return '/new';
  }
};

// Helper function to check if a URL represents a workspace context
export const isWorkspaceURL = (pathname: string): boolean => {
  const urlPath = parseURLPath(pathname);
  return urlPath.type === 'new_session' && !!urlPath.workspaceId;
};

// Helper function to extract workspace ID from URL
export const extractWorkspaceId = (pathname: string): string => {
  const urlPath = parseURLPath(pathname);
  return urlPath.type === 'new_session' ? urlPath.workspaceId : '';
};

// Helper function to extract session ID from URL
export const extractSessionId = (pathname: string): string | undefined => {
  const urlPath = parseURLPath(pathname);
  return urlPath.type === 'existing_session' ? urlPath.sessionId : undefined;
};

// Helper function to check if URL represents an existing session
export const isExistingSessionURL = (pathname: string): boolean => {
  const urlPath = parseURLPath(pathname);
  return urlPath.type === 'existing_session';
};

// Helper function to check if URL represents a new session
export const isNewSessionURL = (pathname: string): boolean => {
  const urlPath = parseURLPath(pathname);
  return urlPath.type === 'new_session';
};

// Helper function to check if URL represents a new non-temporary session
export const isNewNonTemporarySessionURL = (pathname: string): boolean => {
  const urlPath = parseURLPath(pathname);
  return urlPath.type === 'new_session' && !urlPath.isTemporary;
};

// Helper function to check if URL represents a new temporary session
export const isNewTemporarySessionURL = (pathname: string): boolean => {
  const urlPath = parseURLPath(pathname);
  return urlPath.type === 'new_session' && urlPath.isTemporary;
};
