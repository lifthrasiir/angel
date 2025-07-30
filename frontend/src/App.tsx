import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom';

import ChatLayout from './components/ChatLayout';
import SettingsPage from './pages/SettingsPage';

function App() {
  return (
    <Router>
      <Routes>
        <Route path="/" element={<Navigate to="/new" replace />} />
        <Route path="/new" element={<ChatLayout />} />
        <Route path="/:sessionId" element={<ChatLayout />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Routes>
    </Router>
  );
}

export default App;