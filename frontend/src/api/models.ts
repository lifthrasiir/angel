export const getAvailableModels = async (): Promise<string[]> => {
  const response = await fetch('/api/models');
  if (!response.ok) {
    throw new Error(`Failed to fetch models: ${response.statusText}`);
  }
  return response.json();
};
