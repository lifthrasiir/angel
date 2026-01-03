import { atom } from 'jotai';
import { ModelInfo } from '../api/models';
import { PredefinedPrompt } from '../components/chat/SystemPromptEditor';

export const availableModelsAtom = atom<Map<string, ModelInfo>>(new Map());
export const selectedModelAtom = atom<ModelInfo | null>(null);
export const globalPromptsAtom = atom<PredefinedPrompt[]>([]);
export const selectedGlobalPromptAtom = atom<string>('');
