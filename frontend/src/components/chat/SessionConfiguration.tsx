import type React from 'react';
import { useCallback, useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { useAtom, useSetAtom } from 'jotai';
import { availableModelsAtom, selectedModelAtom } from '../../atoms/modelAtoms';
import { messagesAtom, systemPromptAtom } from '../../atoms/chatAtoms';
import { isSessionConfigOpenAtom, sessionConfigTabAtom, isModelManuallySelectedAtom } from '../../atoms/uiAtoms';
import { apiFetch } from '../../api/apiClient';
import { isNewSessionURL } from '../../utils/urlSessionMapping';
import { PredefinedPrompt } from './SystemPromptEditor';
import SystemPromptEditor from './SystemPromptEditor';
import './SessionConfiguration.css';

interface SessionConfigurationProps {
  workspaceId?: string;
  predefinedPrompts?: PredefinedPrompt[];
  sessionId?: string;
}

const ModelTab: React.FC = () => {
  const [availableModels] = useAtom(availableModelsAtom);
  const [selectedModel, setSelectedModel] = useAtom(selectedModelAtom);
  const setIsModelManuallySelected = useSetAtom(isModelManuallySelectedAtom);

  return (
    <div className="config-form-group">
      <label htmlFor="config-model-select">Model:</label>
      <select
        id="config-model-select"
        value={selectedModel?.name || ''}
        onChange={(e) => {
          const selectedModelName = e.target.value;
          const model = availableModels.get(selectedModelName);
          if (model) {
            setSelectedModel(model);
            setIsModelManuallySelected(true);
          }
        }}
        className="config-select"
      >
        {Array.from(availableModels.values()).map((model) => (
          <option key={model.name} value={model.name}>
            {model.name}
          </option>
        ))}
      </select>
    </div>
  );
};

const PromptTabContent: React.FC<{
  workspaceId?: string;
  predefinedPrompts?: PredefinedPrompt[];
}> = ({ workspaceId, predefinedPrompts }) => {
  const [systemPrompt, setSystemPrompt] = useAtom(systemPromptAtom);
  const [messages] = useAtom(messagesAtom);
  const isEditing = messages.length === 0;

  const handlePromptUpdate = useCallback(
    (prompt: PredefinedPrompt) => {
      setSystemPrompt(prompt.value);
      // Send to backend
      apiFetch('/api/systemPrompt', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt: prompt.value, workspaceId }),
      }).catch((error) => {
        console.error('Error updating system prompt:', error);
      });
    },
    [setSystemPrompt, workspaceId],
  );

  return (
    <SystemPromptEditor
      initialPrompt={systemPrompt}
      currentLabel={predefinedPrompts?.find((p) => p.value === systemPrompt)?.label || 'Custom'}
      onPromptUpdate={handlePromptUpdate}
      isEditing={isEditing}
      predefinedPrompts={predefinedPrompts}
      workspaceId={workspaceId}
    />
  );
};

const SessionConfiguration: React.FC<SessionConfigurationProps> = ({ workspaceId, predefinedPrompts, sessionId }) => {
  const [isOpen, setIsOpen] = useAtom(isSessionConfigOpenAtom);
  const [activeTab, setActiveTab] = useAtom(sessionConfigTabAtom);
  const location = useLocation();

  // Auto-open drawer for yet-to-be-created sessions, close for existing or newly created sessions
  useEffect(() => {
    const isNewSession = isNewSessionURL(location.pathname);
    setIsOpen(isNewSession);
  }, [sessionId, location.pathname, setIsOpen]);

  return (
    <div className={`session-config-drawer ${isOpen ? 'open' : ''}`}>
      <div className="session-config-tabs">
        <button className={`config-tab ${activeTab === 'model' ? 'active' : ''}`} onClick={() => setActiveTab('model')}>
          Model
        </button>
        <button
          className={`config-tab ${activeTab === 'prompt' ? 'active' : ''}`}
          onClick={() => setActiveTab('prompt')}
        >
          Prompt
        </button>
      </div>
      <div className="session-config-content">
        {activeTab === 'model' && <ModelTab />}
        {activeTab === 'prompt' && <PromptTabContent workspaceId={workspaceId} predefinedPrompts={predefinedPrompts} />}
      </div>
    </div>
  );
};

export default SessionConfiguration;
