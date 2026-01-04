import { atom } from 'jotai';

// UI-only state that should be local (global for now, will be migrated)
export const lastAutoDisplayedThoughtIdAtom = atom<string | null>(null);
export const statusMessageAtom = atom<string | null>(null);
export const toastMessageAtom = atom<string | null>(null);
export const isSystemPromptEditingAtom = atom<boolean>(false);
export const editingMessageIdAtom = atom<string | null>(null);
export const isModelManuallySelectedAtom = atom<boolean>(false);
export const isSessionConfigOpenAtom = atom<boolean>(false);
export const sessionConfigTabAtom = atom<'model' | 'prompt'>('model');
