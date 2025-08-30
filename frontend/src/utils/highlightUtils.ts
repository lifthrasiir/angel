import hljs from 'highlight.js/lib/core';
import 'highlight.js/styles/atom-one-light.css'; // Keep CSS static for simplicity
import { useAtomValue } from 'jotai';
import { useMemo } from 'react';
import { highlightJsCommonLoadedAtom } from '../atoms/highlightAtoms';

export const getLanguageFromFilename = (filename: string): string | undefined => {
  const parts = filename.split('.');
  if (parts.length > 1) {
    const ext = parts[parts.length - 1];
    switch (ext) {
      case 'js':
        return 'javascript';
      case 'ts':
        return 'typescript';
      case 'go':
        return 'go';
      case 'py':
        return 'python';
      case 'json':
        return 'json';
      case 'xml':
        return 'xml';
      case 'html':
        return 'xml';
      case 'css':
        return 'css';
      case 'md':
        return 'markdown';
      case 'sh':
        return 'bash';
      case 'yaml':
        return 'yaml';
      case 'yml':
        return 'yaml';
      case 'sql':
        return 'sql';
      case 'java':
        return 'java';
      case 'c':
        return 'c';
      case 'cpp':
        return 'cpp';
      case 'cs':
        return 'csharp';
      case 'php':
        return 'php';
      case 'rb':
        return 'ruby';
      case 'rs':
        return 'rust';
      case 'swift':
        return 'swift';
      case 'kt':
        return 'kotlin';
      case 'vue':
        return 'vue';
      case 'jsx':
        return 'javascript';
      case 'tsx':
        return 'typescript';
      default:
        return undefined;
    }
  }
  return undefined;
};

// This function will be used in React components
export const useHighlightCode = (code: string, language?: string): string => {
  const isCommonLoaded = useAtomValue(highlightJsCommonLoadedAtom);

  return useMemo(() => {
    if (!code) {
      return '';
    }
    // Only use specific language if common languages are loaded and language is registered
    if (isCommonLoaded && language && hljs.getLanguage(language)) {
      return hljs.highlight(code, { language }).value;
    } else {
      // Fallback to auto-detection or plain text if not loaded or language not found
      return hljs.highlightAuto(code).value;
    }
  }, [code, language, isCommonLoaded]);
};

export const loadHighlightJsCommon = async (setHighlightJsCommonLoaded: (value: boolean) => void) => {
  try {
    await import('highlight.js/lib/common');
    setHighlightJsCommonLoaded(true);
  } catch (error) {
    console.error('Failed to load highlight.js common languages:', error);
  }
};
