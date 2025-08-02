import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom';

import ChatLayout from './components/ChatLayout';
import SettingsPage from './pages/SettingsPage';
import { ChatProvider } from './hooks/ChatContext';

import NotFoundPage from './pages/NotFoundPage';

function App() {
  return (
    <Router>
      <ChatProvider>
        <Routes>
          <Route path="/" element={<Navigate to="/new" replace />} />
          <Route path="/new" element={<ChatLayout />} />
          <Route path="/:sessionId" element={<ChatLayout />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Routes>
      </ChatProvider>
    </Router>
  );
}

export default App;