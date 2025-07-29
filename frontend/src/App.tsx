import { useState, useEffect, useRef, useMemo } from 'react';
import {
  BrowserRouter as Router,
  Routes,
  Route,
  useNavigate,
  useParams,
  useLocation,
  Navigate,
} from 'react-router-dom';

import ChatMessage from './components/ChatMessage';
import { ThoughtGroup } from './components/ThoughtGroup'; // Import ThoughtGroup

interface ChatMessage {
  id: string; // Add id field
  role: string;
  parts: { text?: string; functionCall?: any; functionResponse?: any; }[];
  type?: "model" | "thought" | "system" | "user" | "function_call" | "function_response"; // Add type field
}

interface Session {
  id: string;
  last_updated_at: string;
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
  const systemPromptTextareaRef = useRef<HTMLTextAreaElement>(null); // 시스템 프롬프트 textarea ref 추가
  
  const isNavigatingFromNewSession = useRef(false); // New ref to track navigation from new session
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null); // Add this ref
  const navigate = useNavigate();
  const { sessionId: urlSessionId } = useParams();
  const location = useLocation();

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

  // Debounce utility function
  const debounce = (func: Function, delay: number) => {
    let timeout: NodeJS.Timeout;
    return (...args: any[]) => {
      clearTimeout(timeout);
      timeout = setTimeout(() => func(...args), delay);
    };
  };

  // Debounced function for textarea height adjustment
  const debouncedAdjustTextareaHeight = useRef(
    debounce((target: HTMLTextAreaElement) => {
      target.style.height = 'auto';
      target.style.height = target.scrollHeight + 'px';
    }, 100) // 100ms debounce delay
  ).current;

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  // Adjust textarea height when inputMessage changes (e.g., after sending message)
  useEffect(() => {
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
      textareaRef.current.style.height = textareaRef.current.scrollHeight + 'px';
    }
  }, [inputMessage]);

  const adjustSystemPromptTextareaHeight = () => {
    if (systemPromptTextareaRef.current) {
      const textarea = systemPromptTextareaRef.current;
      textarea.style.height = 'auto';
      textarea.style.height = textarea.scrollHeight + 'px';
    }
  };

  // Adjust system prompt textarea height
  useEffect(() => {
    adjustSystemPromptTextareaHeight();
  }, [systemPrompt, isSystemPromptEditing]);

  

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
        fetchDefaultSystemPrompt(); // Fetch default prompt for new session
        // Set flag if directly accessing /new to prevent immediate re-load
        if (location.pathname === '/new') {
          isNavigatingFromNewSession.current = true;
        }
        return; // Crucially, exit here to prevent any fetch calls
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
            // Ensure loaded messages have an ID and correct type
            setMessages((data.history || []).map((msg: any) => {
              const chatMessage: ChatMessage = { ...msg, id: msg.id || crypto.randomUUID() };
              if (msg.type === 'thought') {
                chatMessage.type = 'thought';
              } else if (msg.parts[0].functionCall) { // Check for functionCall object
                chatMessage.type = 'function_call';
                chatMessage.parts[0] = { functionCall: msg.parts[0].functionCall };
              } else {
                chatMessage.type = msg.role; // Default to role for other types
              }
              return chatMessage;
            }));
            // This part might be problematic if currentSessionId is already set from URL
            // if (!currentSessionId) {
            //     navigate(`/${data.sessionId}`, { replace: true });
            // }
          } else if (response.status === 401) {
            handleLogin();
          } else if (response.status === 404) {
            console.warn('Session not found:', currentSessionId);
            setChatSessionId(null);
            setMessages([]);
            setSystemPrompt(''); // Clear system prompt on session not found
            setIsSystemPromptEditing(true); // Enable editing for new session
          } else {
            console.error('Failed to load session:', response.status, response.statusText);
            setChatSessionId(null);
            setMessages([]);
            setSystemPrompt(''); // Clear system prompt on error
            setIsSystemPromptEditing(true); // Enable editing for new session
          }
        } catch (error) {
          console.error('Error loading session:', error);
        setChatSessionId(null);
        setMessages([]);
        setSystemPrompt(''); // Clear system prompt on error
        setIsSystemPromptEditing(true); // Enable editing for new session
        }
      } else {
        // No session ID in URL, clear current session state
        setChatSessionId(null);
        setMessages([]);
        setSystemPrompt(''); // Clear system prompt
        setIsSystemPromptEditing(true); // Enable editing
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

  

  const handleSendMessage = async () => {
    if (!inputMessage.trim()) return; // Only check for empty message

    setIsStreaming(true); // Start streaming

    const userMessage: ChatMessage = { id: crypto.randomUUID(), role: 'user', parts: [{ text: inputMessage }], type: 'user' };
    setMessages((prev) => [...prev, userMessage]);
    setInputMessage('');

    let agentMessageId = crypto.randomUUID();
    setMessages((prev) => [...prev, { id: agentMessageId, role: 'model', parts: [{ text: '' }], type: 'model' }]);

    try {
      let apiUrl = '';
      let requestBody: any = {};

      if (chatSessionId) {
        // Existing session
        apiUrl = '/api/chat/message';
        requestBody = { sessionId: chatSessionId, message: inputMessage };
      } else {
        // New session
        apiUrl = '/api/chat/newSessionAndMessage'; // New endpoint
        requestBody = { message: inputMessage, systemPrompt: systemPrompt };
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
        setMessages((prev) => {
          const newMessages = [...prev];
          const agentMessage = newMessages.find(msg => msg.id === agentMessageId);
          if (agentMessage) {
            agentMessage.role = 'system';
            agentMessage.parts[0].text = 'Failed to send message or receive stream.';
            agentMessage.type = 'system';
          }
          return newMessages;
        });
        return;
      }

      

      const reader = response.body?.getReader();
      if (!reader) {
        setMessages((prev) => {
          const newMessages = [...prev];
          const agentMessage = newMessages.find(msg => msg.id === agentMessageId);
          if (agentMessage) {
            agentMessage.role = 'system';
            agentMessage.parts[0].text = 'Failed to get readable stream reader.';
            agentMessage.type = 'system';
          }
          return newMessages;
        });
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

            setMessages((prev) => {
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

            setMessages((prev) => {
              const newMessages = [...prev];
              const agentMessageIndex = newMessages.findIndex(msg => msg.id === agentMessageId);
              if (agentMessageIndex !== -1) {
                newMessages.splice(agentMessageIndex, 0, { id: newThoughtId, role: 'model', parts: [{ text: thoughtText }], type: 'thought' } as ChatMessage);
              }
              return newMessages;
            });
            setLastAutoDisplayedThoughtId(null);
          } else if (data.startsWith('F\n')) {
            const [functionName, functionArgsJson] = data.substring(2).split('\n', 2);
            const functionArgs = JSON.parse(functionArgsJson);

            setMessages((prev) => {
              const newMessages = [...prev];
              const agentMessageIndex = newMessages.findIndex(msg => msg.id === agentMessageId);
              const message: ChatMessage = { id: crypto.randomUUID(), role: 'model', parts: [{ functionCall: { name: functionName, args: functionArgs } }], type: 'function_call' };
              if (agentMessageIndex !== -1) {
                newMessages.splice(agentMessageIndex, 0, message);
              } else {
                newMessages.push(message);
              }
              return newMessages;
            });
            setLastAutoDisplayedThoughtId(null);
          } else if (data.startsWith('R\n')) {
            const functionResponseRaw = JSON.parse(data.substring(2));

            setMessages((prev) => {
              const newMessages = [...prev];
              const agentMessageIndex = newMessages.findIndex(msg => msg.id === agentMessageId);
              const message: ChatMessage = { id: crypto.randomUUID(), role: 'user', parts: [{ functionResponse: { response: functionResponseRaw } }], type: 'function_response' };
              if (agentMessageIndex !== -1) {
                newMessages.splice(agentMessageIndex, 0, message);
              } else {
                newMessages.push(message);
              }
              return newMessages;
            });
            setLastAutoDisplayedThoughtId(null);
          } else if (data.startsWith('S\n')) {
            const newSessionId = data.substring(2);
            setChatSessionId(newSessionId);
            isNavigatingFromNewSession.current = true; // Set flag before navigating
            navigate(`/${newSessionId}`, { replace: true }); // Navigate immediately
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

    } catch (error) {
      console.error('Error sending message or receiving stream:', error);
      setMessages((prev) => {
        const newMessages = [...prev];
        const agentMessage = newMessages.find(msg => msg.id === agentMessageId);
        if (agentMessage) {
          agentMessage.role = 'system';
          agentMessage.parts[0].text = 'Failed to send message or receive stream.';
          agentMessage.type = 'system';
        } else {
          newMessages.push({ id: crypto.randomUUID(), role: 'system', parts: [{ text: 'Error sending message or receiving stream.' }], type: 'system' });
        }
        return newMessages;
      });
      setIsStreaming(false); // End streaming on error
    }
  };

  // Logic to group consecutive thought messages
  const renderedMessages = useMemo(() => {
    const renderedElements: JSX.Element[] = [];
    let i = 0;
    while (i < messages.length) {
      const currentMessage = messages[i];
      if (currentMessage.type === 'thought') {
        const thoughtGroup: ChatMessage[] = [];
        let j = i;
        while (j < messages.length && messages[j].type === 'thought') {
          thoughtGroup.push(messages[j]);
          j++;
        }
        renderedElements.push(<ThoughtGroup key={`thought-group-${i}`} groupId={`thought-group-${i}`} thoughts={thoughtGroup} isAutoDisplayMode={true} lastAutoDisplayedThoughtId={lastAutoDisplayedThoughtId} />);
        i = j; // Move index past the grouped thoughts
      } else {
        renderedElements.push(
          <ChatMessage
            key={currentMessage.id}
            role={currentMessage.role}
            text={currentMessage.parts[0].text}
            type={currentMessage.type}
            functionCall={currentMessage.parts[0].functionCall}
            functionResponse={currentMessage.parts[0].functionResponse}
          />
        );
        i++;
      }
    }
    return renderedElements;
  }, [messages, lastAutoDisplayedThoughtId]);

  return (
    <div style={{ display: 'flex', height: '100vh', overflow: 'hidden' }}>
      {/* Sidebar */}
      <div style={{ width: '200px', background: '#f0f0f0', padding: '20px', display: 'flex', flexDirection: 'column', alignItems: 'center', borderRight: '1px solid #ccc', boxSizing: 'border-box', overflowY: 'hidden', flexShrink: 0 }}>
        <div style={{ fontSize: '3em', marginBottom: '20px' }}>😇</div>
        {!isLoggedIn ? (
          <button onClick={handleLogin} style={{ width: '100%', padding: '10px', marginBottom: '10px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>Login</button>
        ) : (
          <button onClick={() => navigate('/new')} style={{ width: '100%', padding: '10px', marginBottom: '10px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>New Session</button>
        )}
        <div style={{ width: '100%', marginTop: '20px', borderTop: '1px solid #eee', paddingTop: '20px', flexGrow: 1, overflowY: 'auto' }}>
          <h3>Sessions</h3>
          {sessions && sessions.length === 0 ? (
            <p>No sessions yet.</p>
          ) : (
            <ul style={{ listStyle: 'none', padding: 0, width: '100%' }}>
              {sessions.map((session) => (
                <li key={session.id} style={{ marginBottom: '5px' }}>
                  <button
                    onClick={() => navigate(`/${session.id}`)}
                    style={{
                      width: '100%',
                      padding: '8px',
                      textAlign: 'left',
                      border: '1px solid #ddd',
                      borderRadius: '5px',
                      background: session.id === chatSessionId ? '#e0e0e0' : 'white',
                      cursor: 'pointer',
                      whiteSpace: 'nowrap',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                    }}
                  >
                    {session.id}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      {/* Main Chat Area */}
      <div style={{ flexGrow: 1, display: 'flex', flexDirection: 'column', position: 'relative' }}>
        
        {!isLoggedIn && (
          <div style={{ padding: '20px', textAlign: 'center' }}>
            <p>Login required to start chatting.</p>
          </div>
        )}
        
        {isLoggedIn && (
          <>
            <div style={{ flexGrow: 1, overflowY: 'auto' }}>
              <div style={{ maxWidth: '60em', margin: '0 auto', padding: '20px' }}>
                {/* System Prompt Display/Edit */}
                <div className="chat-message-container system-prompt-message">
                  <div className="chat-bubble system-prompt-bubble">
                    {isSystemPromptEditing && messages.length === 0 ? (
                      <>
                        <textarea
                          ref={systemPromptTextareaRef}
                          value={systemPrompt}
                          onChange={(e) => setSystemPrompt(e.target.value)}
                          onInput={(e) => {
                            const target = e.target as HTMLTextAreaElement;
                            target.style.height = 'auto';
                            target.style.height = target.scrollHeight + 'px';
                          }}
                          disabled={messages.length !== 0} // Disable if session exists
                          className={isSystemPromptEditing ? "system-prompt-textarea-editable" : ""}
                          style={{ width: '100%', resize: 'none', border: 'none', background: 'transparent', outline: 'none' }}
                        />
                        
                      </>
                    ) : (
                      <>
                        <div className="system-prompt-display-non-editable" style={{ whiteSpace: 'pre-wrap' }}>{systemPrompt}</div>
                        {messages.length === 0 && ( // Only show edit button if no messages have been sent
                          <button
                            onClick={() => setIsSystemPromptEditing(true)}
                            style={{
                              marginTop: '10px',
                              padding: '5px 10px',
                              background: '#f0f0f0',
                              border: '1px solid #ccc',
                              borderRadius: '5px',
                              cursor: 'pointer',
                              marginLeft: 'auto',
                              display: 'block',
                            }}
                          >
                            Edit System Prompt
                          </button>
                        )}
                      </>
                    )}
                  </div>
                </div>
                {renderedMessages} {/* Call the new renderMessages function */}
                <div ref={messagesEndRef} />
              </div>
            </div>
            <div style={{ padding: '10px 20px', borderTop: '1px solid #ccc', display: 'flex', alignItems: 'center', position: 'sticky', bottom: 0, background: 'white' }}>
              <textarea
                ref={textareaRef}
                value={inputMessage}
                onChange={(e) => setInputMessage(e.target.value)}
                onInput={(e) => {
                  debouncedAdjustTextareaHeight(e.target as HTMLTextAreaElement);
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && e.ctrlKey && !isStreaming) {
                    e.preventDefault(); // Prevent default Ctrl-Enter behavior
                    handleSendMessage();
                  }
                }}
                placeholder="Enter your message..."
                rows={1} // Start with 1 row
                style={{ flexGrow: 1, padding: '10px', marginRight: '10px', border: '1px solid #eee', borderRadius: '5px', resize: 'none', overflowY: 'hidden' }}
              />
              <button onClick={handleSendMessage} disabled={isStreaming} style={{ padding: '10px 20px', background: '#007bff', color: 'white', border: 'none', borderRadius: '5px', cursor: isStreaming ? 'not-allowed' : 'pointer', opacity: isStreaming ? 0.5 : 1 }}>
                Send
              </button>
            </div>
          </>
        )}
      </div>
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
