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
      let currentSessionId = urlSessionId;

      if (currentSessionId) {
        try {
          const response = await fetch(`/api/chat/load?sessionId=${currentSessionId}`);
          if (response.ok) {
            const data = await response.json();
            setChatSessionId(data.sessionId);
            // Ensure loaded messages have an ID
            setMessages(data.history.map((msg: any) => ({ ...msg, id: msg.id || crypto.randomUUID() })));
            setIsLoggedIn(true);
          } else if (response.status === 401) {
            handleLogin();
          } else {
            console.error('Failed to load session:', response.statusText);
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
          navigate(`/${data.sessionId}`);
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

    initializeChatSession();
  }, [urlSessionId, navigate, location.search]);

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

    const userMessage: ChatMessage = { id: crypto.randomUUID(), role: 'user', parts: [{ text: inputMessage }] };
    setMessages((prev) => [...prev, userMessage]);
    setInputMessage('');

    let agentMessageIndex = -1;
    setMessages((prev) => {
      const newMessages = [...prev, { id: crypto.randomUUID(), role: 'model', parts: [{ text: '' }] }];
      agentMessageIndex = newMessages.length - 1;
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
          if (agentMessageIndex !== -1 && newMessages[agentMessageIndex]) {
            newMessages[agentMessageIndex] = { id: newMessages[agentMessageIndex].id, role: 'system', parts: [{ text: 'Failed to send message or receive stream.' }] };
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
          if (agentMessageIndex !== -1 && newMessages[agentMessageIndex]) {
            newMessages[agentMessageIndex] = { id: newMessages[agentMessageIndex].id, role: 'system', parts: [{ text: 'Failed to get readable stream reader.' }] };
          }
          return newMessages;
        });
        return;
      }

      const decoder = new TextDecoder('utf-8');
      let buffer = '';
      let accumulatedAgentText = '';

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

          const lines = eventString.split('\n');
          let dataParts: string[] = [];
          let eventType = 'message';

          for (const line of lines) {
            if (line.startsWith('data: ')) {
              dataParts.push(line.substring(6));
            } else if (line.startsWith('event: ')) {
              eventType = line.substring(7);
            }
          }
          let data = dataParts.join('\n');

          if (eventType === 'message') {
            accumulatedAgentText += data;
            setMessages((prev) => {
              const newMessages = [...prev];
              if (agentMessageIndex !== -1 && newMessages[agentMessageIndex]) {
                newMessages[agentMessageIndex].parts[0].text = accumulatedAgentText;
              }
              return newMessages;
            });
          } else if (eventType === 'done') {
            // Stream finished.
          } else if (eventType === 'error') {
            console.error('SSE Error:', data);
            setMessages((prev) => {
              const newMessages = [...prev];
              if (agentMessageIndex !== -1 && newMessages[agentMessageIndex]) {
                newMessages[agentMessageIndex] = { id: newMessages[agentMessageIndex].id, role: 'system', parts: [{ text: `Error: ${data}` }] };
              }
              return newMessages;
            });
            return;
          }
        }
      }

    } catch (error) {
      console.error('Error sending message or receiving stream:', error);
      setMessages((prev) => {
        const newMessages = [...prev];
        if (agentMessageIndex !== -1 && newMessages[agentMessageIndex]) {
          newMessages[agentMessageIndex] = { id: newMessages[agentMessageIndex].id, role: 'system', parts: [{ text: 'Error sending message or receiving stream.' }] };
        } else {
          newMessages.push({ id: crypto.randomUUID(), role: 'system', parts: [{ text: 'Error sending message or receiving stream.' }] });
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
              <ChatMessage key={msg.id} role={msg.role} text={msg.parts[0].text} />
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

