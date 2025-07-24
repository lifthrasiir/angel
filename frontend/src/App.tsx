import { useState, useEffect } from 'react';

interface ChatMessage {
  role: string;
  parts: { text: string }[];
}

function App() {
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [chatSessionId, setChatSessionId] = useState<string | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [inputMessage, setInputMessage] = useState('');

  useEffect(() => {
    const checkLoginStatus = async () => {
      try {
        const response = await fetch('/api/chat/new', { method: 'POST' });
        if (response.ok) {
          setIsLoggedIn(true);
          const data = await response.json();
          setChatSessionId(data.sessionId);
          setMessages([{ role: 'system', parts: [{ text: data.message }] }]);
        } else {
          setIsLoggedIn(false);
        }
      } catch (error) {
        console.error("Failed to check login status:", error);
        setIsLoggedIn(false);
      }
    };
    checkLoginStatus();
  }, []);

  const handleLogin = () => {
    window.location.href = '/login';
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
        setMessages([{ role: 'system', parts: [{ text: data.message }] }]);
      } else {
        setMessages([{ role: 'system', parts: [{ text: 'Failed to start new session.' }] }]);
      }
    } catch (error) {
      console.error("Error starting new chat session:", error);
      setMessages([{ role: 'system', parts: [{ text: 'Error starting new session.' }] }]);
    }
  };

  const handleSendMessage = async () => {
    if (!inputMessage.trim() || !chatSessionId) return;

    const userMessage: ChatMessage = { role: 'user', parts: [{ text: inputMessage }] };
    setMessages((prev) => [...prev, userMessage]);
    setInputMessage('');

    try {
      const response = await fetch('/api/chat/message', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId: chatSessionId, message: inputMessage }),
      });
      if (response.ok) {
        const data = await response.json();
        const agentResponse: ChatMessage = { role: 'model', parts: [{ text: data.response }] };
        setMessages((prev) => [...prev, agentResponse]);
      } else {
        setMessages((prev) => [...prev, { role: 'system', parts: [{ text: 'Failed to send message.' }] }]);
      }
    } catch (error) {
      console.error("Error sending message:", error);
      setMessages((prev) => [...prev, { role: 'system', parts: [{ text: 'Error sending message.' }] }]);
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
            {messages.map((msg, index) => (
              <p key={index}>
                <strong>{msg.role === 'user' ? 'You' : 'Agent'}:</strong> {msg.parts[0].text}
              </p>
            ))}
          </div>
          <div style={{ marginTop: '10px' }}>
            <input
              type="text"
              value={inputMessage}
              onChange={(e) => setInputMessage(e.target.value)}
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

export default App;