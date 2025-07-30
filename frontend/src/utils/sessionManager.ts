import { Session } from '../types/chat';

export const fetchSessions = async (): Promise<Session[]> => {
  try {
    const response = await fetch('/api/chat/sessions');
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

export const loadSession = async (sessionId: string) => {
  try {
    const response = await fetch(`/api/chat/load?sessionId=${sessionId}`);
    if (response.ok) {
      const data = await response.json();
      if (!data) {
        console.error('Received null data from API for session load');
        return null;
      }
      return data;
    } else if (response.status === 401) {
      throw new Error('UNAUTHORIZED');
    } else if (response.status === 404) {
      console.warn('Session not found:', sessionId);
      return null;
    } else {
      console.error('Failed to load session:', response.status, response.statusText);
      return null;
    }
  } catch (error) {
    if (error instanceof Error && error.message === 'UNAUTHORIZED') {
      throw error;
    }
    console.error('Error loading session:', error);
    return null;
  }
};