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

    // Add an empty agent message placeholder and get its index
    let agentMessageIndex = -1;
    setMessages((prev) => {
      const newMessages = [...prev, { role: 'model', parts: [{ text: '' }] }];
      agentMessageIndex = newMessages.length - 1;
      return newMessages;
    });

    try {
      const response = await fetch('/api/chat/message', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId: chatSessionId, message: inputMessage }),
      });

      if (!response.ok) {
        setMessages((prev) => {
          const newMessages = [...prev];
          if (agentMessageIndex !== -1 && newMessages[agentMessageIndex]) {
            newMessages[agentMessageIndex] = { role: 'system', parts: [{ text: 'Failed to send message or receive stream.' }] };
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
            newMessages[agentMessageIndex] = { role: 'system', parts: [{ text: 'Failed to get readable stream reader.' }] };
          }
          return newMessages;
        });
        return;
      }

      const decoder = new TextDecoder('utf-8');
      let buffer = ''; // Buffer to accumulate partial SSE lines
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
          buffer = buffer.substring(newlineIndex + 2); // Remove processed event from buffer

          // Parse the event string
          const lines = eventString.split('\n');
          let data = '';
          let eventType = 'message'; // Default SSE event type

          for (const line of lines) {
            if (line.startsWith('data: ')) {
              data += line.substring(6);
            } else if (line.startsWith('event: ')) {
              eventType = line.substring(7);
            }
          }

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
            // Stream finished. The final text is already accumulated.
            // No explicit action needed here as the last update to messages will be the final one.
          } else if (eventType === 'error') {
            console.error('SSE Error:', data);
            setMessages((prev) => {
              const newMessages = [...prev];
              if (agentMessageIndex !== -1 && newMessages[agentMessageIndex]) {
                newMessages[agentMessageIndex] = { role: 'system', parts: [{ text: `Error: ${data}` }] };
              }
              return newMessages;
            });
            return; // Stop processing on error
          }
        }
      }

    } catch (error) {
      console.error("Error sending message or receiving stream:", error);
      setMessages((prev) => {
        const newMessages = [...prev];
        // If an agent message placeholder was added, update it to an an error message
        if (agentMessageIndex !== -1 && newMessages[agentMessageIndex]) {
          newMessages[agentMessageIndex] = { role: 'system', parts: [{ text: 'Error sending message or receiving stream.' }] };
        } else {
          // Otherwise, just add a new system error message
          newMessages.push({ role: 'system', parts: [{ text: 'Error sending message or receiving stream.' }] });
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