import type { URLPath } from '../types/sessionFSM';

// Parse the current URL path to determine session type and identifiers
export const parseURLPath = (pathname: string): URLPath => {
  // Remove leading slash and split by '/'
  const segments = pathname.replace(/^\//, '').split('/').filter(Boolean);

  // Pattern: /new -> new global session
  if (segments.length === 1 && segments[0] === 'new') {
    return { type: 'new_global' };
  }

  // Pattern: /temp -> new temporary session
  if (segments.length === 1 && segments[0] === 'temp') {
    return { type: 'new_temp' };
  }

  // Pattern: /w/:workspaceId/new -> new workspace session
  if (segments.length === 3 && segments[0] === 'w' && segments[2] === 'new' && segments[1]) {
    return { type: 'new_workspace', workspaceId: segments[1] };
  }

  // Pattern: /:sessionId -> existing session
  if (segments.length === 1 && segments[0]) {
    return { type: 'existing_session', sessionId: segments[0] };
  }

  // Default fallback - treat as new global session
  return { type: 'new_global' };
};

// Convert URLPath back to a URL string (useful for navigation)
export const urlPathToURL = (urlPath: URLPath): string => {
  switch (urlPath.type) {
    case 'new_global':
      return '/new';

    case 'new_temp':
      return '/temp';

    case 'new_workspace':
      return `/w/${urlPath.workspaceId}/new`;

    case 'existing_session':
      return `/${urlPath.sessionId}`;

    default:
      return '/new';
  }
};

// Helper function to check if a URL represents a workspace context
export const isWorkspaceURL = (pathname: string): boolean => {
  const urlPath = parseURLPath(pathname);
  return urlPath.type === 'new_workspace';
};

// Helper function to extract workspace ID from URL
export const extractWorkspaceId = (pathname: string): string | undefined => {
  const urlPath = parseURLPath(pathname);
  return urlPath.type === 'new_workspace' ? urlPath.workspaceId : undefined;
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
  return urlPath.type === 'new_global' || urlPath.type === 'new_temp' || urlPath.type === 'new_workspace';
};
