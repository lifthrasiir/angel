import { apiFetch } from './apiClient';

export interface ModelInfo {
  name: string;
  maxTokens: number;
}

export const getAvailableModels = async (): Promise<Map<string, ModelInfo>> => {
  const response = await apiFetch('/api/models');
  if (!response.ok) {
    throw new Error(`Failed to fetch models: ${response.statusText}`);
  }
  const modelsArray: ModelInfo[] = await response.json();
  const modelsMap = new Map<string, ModelInfo>();
  modelsArray.forEach((model) => {
    modelsMap.set(model.name, model);
  });
  return modelsMap;
};
