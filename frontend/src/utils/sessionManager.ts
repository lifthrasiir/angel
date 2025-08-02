import { Session } from '../types/chat';

export const fetchSessions = async (): Promise<Session[]> => {
  try {
    const response = await fetch('/api/chat');
    if (response.ok) {
      const data: Session[] = await response.json();
      return data;
    } else if (response.status === 401) {
      return [];
    } else {
      console.error('Failed to fetch sessions:', response.status, response.statusText);
      return [];
    }
  } catch (error) {
    console.error('Error fetching sessions:', error);
    return [];
  }
};

export const loadSession = (sessionId: string, onMessage: (event: MessageEvent) => void, onError: (event: Event) => void): EventSource => {
  const eventSource = new EventSource(`/api/chat/${sessionId}`, {
    withCredentials: true,
  });

  eventSource.onmessage = onMessage;
  eventSource.onerror = onError;

  return eventSource;
};