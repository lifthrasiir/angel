import { atom } from 'jotai';

export const selectedFilesAtom = atom<File[]>([]);
export const preserveSelectedFilesAtom = atom<File[]>([]);
export const pendingRootsAtom = atom<string[]>([]);
