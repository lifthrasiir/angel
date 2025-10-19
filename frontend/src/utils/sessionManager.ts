import type { Session, WorkspaceWithSessions } from '../types/chat';
import { apiFetch } from '../api/apiClient';

export const fetchSessions = async (workspaceId?: string): Promise<WorkspaceWithSessions> => {
  let url = '/api/chat';
  if (workspaceId) {
    url = `/api/chat?workspaceId=${workspaceId}`;
  }
  const response = await apiFetch(url);
  if (!response.ok) {
    if (response.status === 401) {
      throw new Error('Unauthorized');
    }
    throw new Error(`Failed to fetch sessions: ${response.status} ${response.statusText}`);
  }
  const data: WorkspaceWithSessions = await response.json();
  return data;
};

export const loadSession = (
  sessionId: string,
  fetchLimit: number,
  onMessage: (event: MessageEvent) => void,
  onError: (event: Event) => void,
): EventSource => {
  const eventSource = new EventSource(`/api/chat/${sessionId}?fetchLimit=${fetchLimit}`, {
    withCredentials: true,
  });

  eventSource.onmessage = onMessage;
  eventSource.onerror = onError;

  return eventSource;
};

export const fetchSession = async (sessionId: string): Promise<Session | null> => {
  const response = await apiFetch(`/api/chat/${sessionId}/info`); // Assuming an endpoint for single session info
  if (!response.ok) {
    if (response.status === 404) {
      return null; // Session not found
    }
    throw new Error(`Failed to fetch session info: ${response.status} ${response.statusText}`);
  }
  const data: Session = await response.json();
  return data;
};
