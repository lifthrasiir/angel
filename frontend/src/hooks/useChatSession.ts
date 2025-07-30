import { useState } from 'react';
import { useLocation } from 'react-router-dom';
import { ChatMessage, Session } from '../types/chat';
import { fetchSessions } from '../utils/sessionManager';
import { handleFilesSelected, handleRemoveFile } from '../utils/fileHandler';
import { handleLogin } from '../utils/userManager';
import { useDocumentTitle } from './useDocumentTitle';
import { useSessionInitialization } from './useSessionInitialization';
import { useMessageSending } from './useMessageSending';

export const useChatSession = () => {
  const [userEmail, setUserEmail] = useState<string | null>(null);
  const [chatSessionId, setChatSessionId] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [inputMessage, setInputMessage] = useState('');
  const [sessions, setSessions] = useState<Session[]>([]);
  const [lastAutoDisplayedThoughtId, setLastAutoDisplayedThoughtId] = useState<string | null>(null);
  const [isStreaming, setIsStreaming] = useState(false);
  const [systemPrompt, setSystemPrompt] = useState<string>('');
  const [isSystemPromptEditing, setIsSystemPromptEditing] = useState(false);
  const [selectedFiles, setSelectedFiles] = useState<File[]>([]);
  
  const location = useLocation();

  const loadSessions = async () => {
    const sessionsData = await fetchSessions();
    setSessions(sessionsData);
  };

  const handleLoginRedirect = () => {
    const currentPath = location.pathname + location.search;
    handleLogin(currentPath, inputMessage);
  };

  const handleFilesSelectedWrapper = (files: File[]) => {
    setSelectedFiles((prev) => handleFilesSelected(prev, files));
  };

  const handleRemoveFileWrapper = (index: number) => {
    setSelectedFiles((prev) => handleRemoveFile(prev, index));
  };

  useDocumentTitle(sessions);

  useSessionInitialization({
    chatSessionId,
    isStreaming,
    setInputMessage,
    setChatSessionId,
    setMessages,
    setSystemPrompt,
    setIsSystemPromptEditing,
    setSelectedFiles,
    setIsStreaming,
    setUserEmail,
    handleLoginRedirect,
    loadSessions,
  });

  const { handleSendMessage } = useMessageSending({
    inputMessage,
    selectedFiles,
    chatSessionId,
    systemPrompt,
    setInputMessage,
    setSelectedFiles,
    setIsStreaming,
    setMessages,
    setLastAutoDisplayedThoughtId,
    setChatSessionId,
    setSessions,
    setIsSystemPromptEditing,
    handleLoginRedirect,
    loadSessions,
  });

  return {
    userEmail,
    chatSessionId,
    messages,
    inputMessage,
    sessions,
    setSessions,
    lastAutoDisplayedThoughtId,
    isStreaming,
    systemPrompt,
    isSystemPromptEditing,
    selectedFiles,
    setInputMessage,
    setSystemPrompt,
    setIsSystemPromptEditing,
    handleLogin: handleLoginRedirect,
    handleFilesSelected: handleFilesSelectedWrapper,
    handleRemoveFile: handleRemoveFileWrapper,
    handleSendMessage,
    fetchSessions: loadSessions,
  };
};