import { atom } from 'jotai';
import type { ChatMessage } from '../types/chat';

export const pendingConfirmationAtom = atom<string | null>(null);
export const temporaryEnvChangeMessageAtom = atom<ChatMessage | null>(null);
