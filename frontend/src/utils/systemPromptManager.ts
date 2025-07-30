export const fetchDefaultSystemPrompt = async (): Promise<string> => {
  try {
    const response = await fetch('/api/default-system-prompt');
    if (response.ok) {
      const data = await response.text();
      return data;
    } else {
      console.error('Failed to fetch default system prompt:', response.status, response.statusText);
      return '';
    }
  } catch (error) {
    console.error('Error fetching default system prompt:', error);
    return '';
  }
};