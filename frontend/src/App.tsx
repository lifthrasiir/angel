import { useState, useEffect, useRef } from 'react';
import {
  BrowserRouter as Router,
  Routes,
  Route,
  useNavigate,
  useParams,
  useLocation,
  Navigate,
} from 'react-router-dom';

import Sidebar from './components/Sidebar';
import ChatArea from './components/ChatArea';
import { FileAttachment } from './components/FileAttachmentPreview';

interface ChatMessage {
  id: string; // Add id field
  role: string;
  parts: { text?: string; functionCall?: any; functionResponse?: any; }[];
  type?: "model" | "thought" | "system" | "user" | "function_call" | "function_response"; // Add type field
  attachments?: FileAttachment[]; // New field
}

interface Session {
  id: string;
  last_updated_at: string;
  name?: string;
  isEditing?: boolean;
}

function ChatApp() {
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [chatSessionId, setChatSessionId] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [inputMessage, setInputMessage] = useState('');
  const [sessions, setSessions] = useState<Session[]>([]); // New state for sessions
  const [lastAutoDisplayedThoughtId, setLastAutoDisplayedThoughtId] = useState<string | null>(null);
  const [isStreaming, setIsStreaming] = useState(false); // New state for streaming status
  const [systemPrompt, setSystemPrompt] = useState<string>(''); // 시스템 프롬프트 상태 추가
  const [isSystemPromptEditing, setIsSystemPromptEditing] = useState(false); // 시스템 프롬프트 편집 모드 상태 추가
  const [sessionName, setSessionName] = useState(''); // 세션 이름 상태 추가
  const [selectedFiles, setSelectedFiles] = useState<File[]>([]); // New state for selected files
  
  const isNavigatingFromNewSession = useRef(false); // New ref to track navigation from new session
  const navigate = useNavigate();
  const { sessionId: urlSessionId } = useParams();
  const location = useLocation();

  // Helper function to update messages state
  const updateMessagesState = (
    newMessage: ChatMessage,
    options?: { replaceId?: string; insertBeforeAgentId?: string }
  ) => {
    setMessages((prev) => {
      const newMessages = [...prev];
      let insertIndex = newMessages.length; // Default to appending

      if (options?.replaceId) {
        const indexToReplace = newMessages.findIndex(msg => msg.id === options.replaceId);
        if (indexToReplace !== -1) {
          newMessages[indexToReplace] = newMessage;
          return newMessages; // Return early if replaced
        }
      }

      if (options?.insertBeforeAgentId) {
        const agentMessageIndex = newMessages.findIndex(msg => msg.id === options.insertBeforeAgentId);
        if (agentMessageIndex !== -1) {
          insertIndex = agentMessageIndex;
        }
      }

      newMessages.splice(insertIndex, 0, newMessage);
      return newMessages;
    });
  };

  const fetchDefaultSystemPrompt = async () => {
    try {
      const response = await fetch('/api/default-system-prompt');
      if (response.ok) {
        const data = await response.text();
        setSystemPrompt(data);
      } else {
        console.error('Failed to fetch default system prompt:', response.status, response.statusText);
      }
    } catch (error) {
      console.error('Error fetching default system prompt:', error);
    }
  };

  const fetchSessions = async () => {
    
    try {
      const response = await fetch('/api/chat/sessions');
      if (response.ok) {
        const data: Session[] = await response.json();
        setSessions(data);
        setIsLoggedIn(true); // Set isLoggedIn to true on successful fetch
        
      } else if (response.status === 401) {
        // Not logged in, clear sessions
        setSessions([]);
        setIsLoggedIn(false); // Set isLoggedIn to false on 401
      } else {
        console.error('Failed to fetch sessions:', response.status, response.statusText);
        setIsLoggedIn(false); // Also set to false on other errors
      }
    } catch (error) {
      console.error('Error fetching sessions:', error);
    }
  };

  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const redirectTo = params.get('redirect_to');
    const draftMessage = params.get('draft_message');

    if (draftMessage) {
      setInputMessage(draftMessage);
    }

    if (redirectTo) {
      if (redirectTo.startsWith('/')) {
        navigate(redirectTo, { replace: true });
      } else {
        console.warn('Invalid redirectTo URL detected, redirecting to home:', redirectTo);
        navigate('/', { replace: true });
      }
      return;
    }

    const initializeChatSession = async () => {
      if (isNavigatingFromNewSession.current) {
        isNavigatingFromNewSession.current = false; // Reset flag
        return; // Skip session loading as we are navigating from a new session
      }

      let currentSessionId = urlSessionId;
      if (!currentSessionId && location.pathname === '/new') {
        currentSessionId = 'new';
      }

      // If the URL is /new, we don't try to load a session.
      if (currentSessionId === 'new') {
        setChatSessionId(null);
        setMessages([]); // Clear messages for a truly new session
        setSystemPrompt(''); // Clear system prompt for a new session
        setIsSystemPromptEditing(true); // Enable editing for new session
        setSessionName(''); // Clear session name for a new session
        setSelectedFiles([]); // Clear selected files for a new session
        fetchDefaultSystemPrompt(); // Fetch default prompt for new session
        // Set flag if directly accessing /new to prevent immediate re-load
        if (location.pathname === '/new') {
          isNavigatingFromNewSession.current = true;
        }
        return; // Crucially, exit here to prevent any fetch calls
      }

      // If currentSessionId changes, clear selected files
      if (currentSessionId !== chatSessionId) {
        setSelectedFiles([]);
      }

      if (currentSessionId) {
        try {
          const response = await fetch(`/api/chat/load?sessionId=${currentSessionId}`);
          if (response.ok) {
            const data = await response.json();
            if (!data) {
                console.error('Received null data from API for session load');
                return;
            }
            setChatSessionId(data.sessionId);
            setSystemPrompt(data.systemPrompt); // Load system prompt directly
            if (!data.systemPrompt) { // If system prompt is empty, fetch default
              fetchDefaultSystemPrompt();
            }
            setIsSystemPromptEditing(false); // Disable editing for existing session
            setSessionName(data.name || ''); // Load session name
            // Ensure loaded messages have an ID and correct type
            setMessages((data.history || []).map((msg: any) => {
              const chatMessage: ChatMessage = { ...msg, id: msg.id || crypto.randomUUID(), attachments: msg.attachments };
              if (msg.type === 'thought') {
                chatMessage.type = 'thought';
              } else if (msg.parts[0].functionCall) { // Check for functionCall object
                chatMessage.type = 'function_call';
                chatMessage.parts[0] = { functionCall: msg.parts[0].functionCall };
              } else if (msg.parts[0].functionResponse) { // Check for functionResponse object
                chatMessage.type = 'function_response';
                chatMessage.parts[0] = { functionResponse: msg.parts[0].functionResponse };
              } else {
                chatMessage.type = msg.role; // Default to role for other types
              }
              return chatMessage;
            }));
            } else if (response.status === 401) {
            handleLogin();
          } else if (response.status === 404) {
            console.warn('Session not found:', currentSessionId);
            setChatSessionId(null);
            setMessages([]);
            setSystemPrompt(''); // Clear system prompt on session not found
            setIsSystemPromptEditing(true); // Enable editing for new session
            setSessionName(''); // Clear session name on session not found
          } else {
            console.error('Failed to load session:', response.status, response.statusText);
            setChatSessionId(null);
            setMessages([]);
            setSystemPrompt(''); // Clear system prompt on error
            setIsSystemPromptEditing(true); // Enable editing for new session
            setSessionName(''); // Clear session name on error
          }
        } catch (error) {
          console.error('Error loading session:', error);
        setChatSessionId(null);
        setMessages([]);
        setSystemPrompt(''); // Clear system prompt on error
        setIsSystemPromptEditing(true); // Enable editing for new session
        setSessionName(''); // Clear session name on error
        }
      }
 else {
        // No session ID in URL, clear current session state
        setChatSessionId(null);
        setMessages([]);
        setSystemPrompt(''); // Clear system prompt
        setIsSystemPromptEditing(true); // Enable editing
        setSessionName(''); // Clear session name
      }
    };

    initializeChatSession();
    fetchSessions(); // Fetch sessions on initial load
  }, [urlSessionId, navigate, location.search, location.pathname]);


  const handleLogin = () => {
    const currentPath = location.pathname + location.search;
    const draftMessage = inputMessage;
    let redirectToUrl = `/login?redirect_to=${encodeURIComponent(currentPath)}`;

    if (draftMessage) {
      redirectToUrl += `&draft_message=${encodeURIComponent(draftMessage)}`;
    }
    window.location.href = redirectToUrl;
  };

  const handleFilesSelected = (files: File[]) => {
    setSelectedFiles((prev) => [...prev, ...files]);
  };

  const handleRemoveFile = (index: number) => {
    setSelectedFiles((prev) => prev.filter((_, i) => i !== index));
  };

  const handleSendMessage = async () => {
    if (!inputMessage.trim() && selectedFiles.length === 0) return; // Check for empty message AND no files

    setIsStreaming(true); // Start streaming

    try {
      let apiUrl = '';
      let requestBody: any = {};

      const attachments: FileAttachment[] = await Promise.all(
        selectedFiles.map(async (file) => {
          const data = await new Promise<string>((resolve) => {
            const reader = new FileReader();
            reader.onloadend = () => {
              resolve(reader.result?.toString().split(',')[1] || ''); // Get base64 part
            };
            reader.readAsDataURL(file);
          });
          return { fileName: file.name, mimeType: file.type, data };
        })
      );

    const userMessage: ChatMessage = { id: crypto.randomUUID(), role: 'user', parts: [{ text: inputMessage }], type: 'user', attachments: attachments };
    updateMessagesState(userMessage); // Use helper
    setInputMessage('');
    setSelectedFiles([]); // Clear selected files immediately after sending message

    let agentMessageId = crypto.randomUUID();
    updateMessagesState({ id: agentMessageId, role: 'model', parts: [{ text: '' }], type: 'model' } as ChatMessage); // Use helper


      if (chatSessionId) {
        // Existing session
        apiUrl = '/api/chat/message';
        requestBody = { sessionId: chatSessionId, message: inputMessage, attachments };
      } else {
        // New session
        apiUrl = '/api/chat/newSessionAndMessage'; // New endpoint
        requestBody = { message: inputMessage, systemPrompt: systemPrompt, name: sessionName, attachments };
          }

          // 첫 메시지 전송 후 시스템 프롬프트 편집 비활성화
          if (chatSessionId === null) {
            setIsSystemPromptEditing(false);
          }

      const response = await fetch(apiUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
      });

      if (response.status === 401) {
        handleLogin();
        return;
      }

      if (!response.ok) {
        updateMessagesState({
          id: agentMessageId, // Use the agentMessageId to replace the placeholder
          role: 'system',
          parts: [{ text: 'Failed to send message or receive stream.' }],
          type: 'system',
        } as ChatMessage, { replaceId: agentMessageId });
        return;
      }

      

      const reader = response.body?.getReader();
      if (!reader) {
        updateMessagesState({
          id: agentMessageId, // Use the agentMessageId to replace the placeholder
          role: 'system',
          parts: [{ text: 'Failed to get readable stream reader.' }],
          type: 'system',
        }, { replaceId: agentMessageId });
        return;
      }

      const decoder = new TextDecoder('utf-8');
      let buffer = '';
      let currentAgentText = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) {
          break;
        }
        buffer += decoder.decode(value, { stream: true });

        let newlineIndex;
        while ((newlineIndex = buffer.indexOf('\n\n')) !== -1) {
          const eventString = buffer.substring(0, newlineIndex);
          buffer = buffer.substring(newlineIndex + 2);

          const data = eventString.slice(6).replace(/\ndata: /g, '\n');

          if (data.startsWith('M\n')) {
            currentAgentText += data.substring(2); // Remove "M\n"

            setMessages((prev) => { // This one is an update, not a push/splice
              const newMessages = [...prev];
              const agentMessage = newMessages.find(msg => msg.id === agentMessageId);
              if (agentMessage) {
                agentMessage.parts[0].text = currentAgentText;
              }
              return newMessages;
            });
            setLastAutoDisplayedThoughtId(null);
          } else if (data.startsWith('T\n')) {
            const thoughtText = data.substring(2);
            const newThoughtId = crypto.randomUUID();

            updateMessagesState(
              { id: newThoughtId, role: 'model', parts: [{ text: thoughtText }], type: 'thought' } as ChatMessage,
              { insertBeforeAgentId: agentMessageId }
            );
            setLastAutoDisplayedThoughtId(newThoughtId);
          } else if (data.startsWith('F\n')) {
            const [functionName, functionArgsJson] = data.substring(2).split('\n', 2);
            const functionArgs = JSON.parse(functionArgsJson);
            const message: ChatMessage = { id: crypto.randomUUID(), role: 'model', parts: [{ functionCall: { name: functionName, args: functionArgs } }], type: 'function_call' };

            updateMessagesState(
              message,
              { insertBeforeAgentId: agentMessageId }
            );
            setLastAutoDisplayedThoughtId(null);
          } else if (data.startsWith('R')) { // Changed from 'R\n' to 'R' to catch all R events
            const functionResponseRaw = JSON.parse(data.substring(2));
            const message: ChatMessage = { id: crypto.randomUUID(), role: 'user', parts: [{ functionResponse: { response: functionResponseRaw } }], type: 'function_response' };

            updateMessagesState(
              message,
              { insertBeforeAgentId: agentMessageId }
            );
            setLastAutoDisplayedThoughtId(null);
          } else if (data.startsWith('S\n')) {
            const newSessionId = data.substring(2);
            setChatSessionId(newSessionId);
            isNavigatingFromNewSession.current = true; // Set flag before navigating
            navigate(`/${newSessionId}`, { replace: true }); // Navigate immediately
          } else if (data.startsWith('N\n')) { // New: Session Name Update
            const [sessionIdToUpdate, newName] = data.substring(2).split('\n', 2);
            setSessions(prevSessions =>
              prevSessions.map(s =>
                s.id === sessionIdToUpdate ? { ...s, name: newName } : s
              )
            );
            // No need to call fetchSessions() here, as setSessions already updates the state.
          } else if (data === 'Q') {
            // End of content signal
            setLastAutoDisplayedThoughtId(null);
            break;
          } else {
            console.warn('Unknown protocol:', data);
          }
        }
      }
      fetchSessions(); // Refresh sessions after sending message
      setIsStreaming(false); // End streaming
      setSelectedFiles([]); // Clear selected files after sending

    } catch (error) {
      console.error('Error sending message or receiving stream:', error);
      updateMessagesState({
        id: crypto.randomUUID(),
        role: 'system',
        parts: [{ text: 'Error sending message or receiving stream.' }],
        type: 'system',
      } as ChatMessage);
      setIsStreaming(false); // End streaming on error
    }
  };

  return (
    <div style={{ display: 'flex', height: '100vh', overflow: 'hidden' }}>
      <Sidebar
        isLoggedIn={isLoggedIn}
        handleLogin={handleLogin}
        sessions={sessions}
        setSessions={setSessions}
        chatSessionId={chatSessionId}
        fetchSessions={fetchSessions}
      />

      <ChatArea
        isLoggedIn={isLoggedIn}
        messages={messages}
        lastAutoDisplayedThoughtId={lastAutoDisplayedThoughtId}
        systemPrompt={systemPrompt}
        setSystemPrompt={setSystemPrompt}
        isSystemPromptEditing={isSystemPromptEditing}
        setIsSystemPromptEditing={setIsSystemPromptEditing}
        inputMessage={inputMessage}
        setInputMessage={setInputMessage}
        handleSendMessage={handleSendMessage}
        isStreaming={isStreaming}
        onFilesSelected={handleFilesSelected}
        selectedFiles={selectedFiles}
        handleRemoveFile={handleRemoveFile}
      />
      
      </div>
  );
}

function App() {
  return (
    <Router>
      <Routes>
        <Route path="/" element={<Navigate to="/new" replace />} />
        <Route path="/new" element={<ChatApp />} />
        <Route path="/:sessionId" element={<ChatApp />} />
      </Routes>
    </Router>
  );
}

export default App;