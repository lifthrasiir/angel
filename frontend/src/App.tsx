import { useState, useEffect, useRef } from 'react';
import {
  BrowserRouter as Router,
  Routes,
  Route,
  useNavigate,
  useParams,
  useLocation,
} from 'react-router-dom';

import ChatMessage from './components/ChatMessage';

interface ChatMessage {
  id: string; // Add id field
  role: string;
  parts: { text: string }[];
  type?: "model" | "thought" | "system" | "user"; // Add type field
}

function ChatApp() {
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [chatSessionId, setChatSessionId] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [inputMessage, setInputMessage] = useState('');
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();
  const { sessionId: urlSessionId } = useParams();
  const location = useLocation();
  const [isSessionInitialized, setIsSessionInitialized] = useState(false); // New state

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

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
      if (isSessionInitialized) return; // Prevent re-initialization

      let currentSessionId = urlSessionId;

      if (currentSessionId) {
        try {
          const response = await fetch(`/api/chat/load?sessionId=${currentSessionId}`);
          if (response.ok) {
            const data = await response.json();
            setChatSessionId(data.sessionId);
            // Ensure loaded messages have an ID
            setMessages(data.history.map((msg: any) => {
              const chatMessage: ChatMessage = { ...msg, id: msg.id || crypto.randomUUID() };
              if (chatMessage.role === 'thought') {
                chatMessage.type = 'thought';
                // Reconstruct the thought message text for display
                const parts = chatMessage.parts[0].text.split('\n');
                const subject = parts[0];
                const description = parts.slice(1).join('\n');
                chatMessage.parts[0].text = `**Thought: ${subject}**\n${description}`;
              }
              return chatMessage;
            }));
            setIsLoggedIn(true);
            setIsSessionInitialized(true); // Mark as initialized
            if (currentSessionId !== data.sessionId) {
                navigate(`/${data.sessionId}`, { replace: true });
            }
          } else if (response.status === 401) {
            handleLogin();
          } else {
            console.error('Failed to load session:', response.status, response.statusText);
            await startNewSession();
          }
        } catch (error) {
          console.error('Error loading session:', error);
          await startNewSession();
        }
      } else {
        await startNewSession();
      }
    };

    const startNewSession = async () => {
      try {
        const response = await fetch('/api/chat/new', { method: 'POST' });
        if (response.ok) {
          const data = await response.json();
          setChatSessionId(data.sessionId);
          setMessages([{ id: crypto.randomUUID(), role: 'system', parts: [{ text: data.message }] }]);
          setIsLoggedIn(true);
          setIsSessionInitialized(true); // Mark as initialized
          navigate(`/${data.sessionId}`, { replace: true }); // Use replace to avoid history stack issues
        } else if (response.status === 401) {
          handleLogin();
        } else {
          setIsLoggedIn(false);
          setMessages([{ id: crypto.randomUUID(), role: 'system', parts: [{ text: 'Failed to start new session.' }] }]);
        }
      } catch (error) {
        console.error('Error starting new chat session:', error);
        setIsLoggedIn(false);
        setMessages([{ id: crypto.randomUUID(), role: 'system', parts: [{ text: 'Error starting new session.' }] }]);
      }
    };

    // Only run initialization if not already initialized and not handling a redirect
    if (!isSessionInitialized && !location.search.includes('redirect_to')) {
      initializeChatSession();
    }
  }, [urlSessionId, navigate, location.search, isSessionInitialized]);


  const handleLogin = () => {
    const currentPath = location.pathname + location.search;
    const draftMessage = inputMessage;
    let redirectToUrl = `/login?redirect_to=${encodeURIComponent(currentPath)}`;

    if (draftMessage) {
      redirectToUrl += `&draft_message=${encodeURIComponent(draftMessage)}`;
    }
    window.location.href = redirectToUrl;
  };

  const handleNewChatSession = async () => {
    try {
      const response = await fetch('/api/chat/new', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      });
      if (response.ok) {
        const data = await response.json();
        setChatSessionId(data.sessionId);
        setMessages([{ id: crypto.randomUUID(), role: 'system', parts: [{ text: data.message }] }]);
        navigate(`/${data.sessionId}`);
      } else if (response.status === 401) {
        handleLogin();
      } else {
        setMessages([{ id: crypto.randomUUID(), role: 'system', parts: [{ text: 'Failed to start new session.' }] }]);
      }
    } catch (error) {
      console.error('Error starting new chat session:', error);
      setMessages([{ id: crypto.randomUUID(), role: 'system', parts: [{ text: 'Error starting new session.' }] }]);
    }
  };

  const handleSendMessage = async () => {
    if (!inputMessage.trim() || !chatSessionId) return;

    const userMessage: ChatMessage = { id: crypto.randomUUID(), role: 'user', parts: [{ text: inputMessage }], type: 'user' };
    setMessages((prev) => [...prev, userMessage]);
    setInputMessage('');

    let agentMessageId = crypto.randomUUID();
    setMessages((prev) => {
      const newMessages = [...prev, { id: agentMessageId, role: 'model', parts: [{ text: '' }], type: 'model' } as ChatMessage];
      return newMessages;
    });

    try {
      const response = await fetch('/api/chat/message', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId: chatSessionId, message: inputMessage }),
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
        console.error('Failed to get readable stream reader.');
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
          } else if (data.startsWith('T\n')) {
            const parts = data.substring(2).split('\n'); // Remove "T\n" and split by newline
            const subject = parts[0];
            const description = parts.slice(1).join('\n');

            setMessages((prev) => {
              const newMessages = [...prev];
              const agentMessageIndex = newMessages.findIndex(msg => msg.id === agentMessageId);
              if (agentMessageIndex !== -1) {
                newMessages.splice(agentMessageIndex, 0, { id: crypto.randomUUID(), role: 'system', parts: [{ text: `**Thought: ${subject}**
${description}` }], type: 'thought' } as ChatMessage);
              }
              return newMessages;
            });
          } else if (data === 'Q') {
            // End of content signal
            break;
          } else {
            console.warn('Unknown protocol:', data);
          }
        }
      }

    } catch (error) {
      console.error('Error sending message or receiving stream:', error);
      setMessages((prev) => {
        const newMessages = [...prev];
        const agentMessage = newMessages.find(msg => msg.id === agentMessageId);
        if (agentMessage) {
          agentMessage.role = 'system';
          agentMessage.parts[0].text = 'Error sending message or receiving stream.';
          agentMessage.type = 'system';
        } else {
          newMessages.push({ id: crypto.randomUUID(), role: 'system', parts: [{ text: 'Error sending message or receiving stream.' }], type: 'system' });
        }
        return newMessages;
      });
    }
  };

  return (
    <div style={{ padding: '20px', maxWidth: '800px', margin: 'auto' }}>
      <h1>Angel CLI Web</h1>
      {!isLoggedIn ? (
        <div>
          <p>Login required.</p>
          <button onClick={handleLogin}>Login with Google Account</button>
        </div>
      ) : (
        <div>
          <p>Logged in. Session ID: {chatSessionId || 'None'}</p>
          <button onClick={handleNewChatSession}>Start New Chat Session</button>
          <div style={{ border: '1px solid #ccc', padding: '10px', marginTop: '20px', height: '300px', overflowY: 'scroll' }}>
            {messages.map((msg) => (
              <ChatMessage key={msg.id} role={msg.role} text={msg.parts[0].text} type={msg.type} />
            ))}
            <div ref={messagesEndRef} />
          </div>
          <div style={{ marginTop: '10px' }}>
            <input
              type="text"
              value={inputMessage}
              onChange={(e) => {
                setInputMessage(e.target.value);
              }}
              onKeyPress={(e) => {
                if (e.key === 'Enter') {
                  handleSendMessage();
                }
              }}
              placeholder="Enter your message..."
              style={{ width: 'calc(100% - 80px)', padding: '8px' }}
            />
            <button onClick={handleSendMessage} style={{ width: '70px', padding: '8px', marginLeft: '10px' }}>
              Send
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function App() {
  return (
    <Router>
      <Routes>
        <Route path="/" element={<ChatApp />} />
        <Route path="/:sessionId" element={<ChatApp />} />
      </Routes>
    </Router>
  );
}

export default App;

