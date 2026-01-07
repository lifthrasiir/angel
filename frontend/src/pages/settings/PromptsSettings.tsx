import type React from 'react';
import { useEffect } from 'react';
import { Outlet, useNavigate } from 'react-router-dom';
import { useAtom } from 'jotai';
import { apiFetch } from '../../api/apiClient';
import { globalPromptsAtom } from '../../atoms/modelAtoms';
import type { PredefinedPrompt } from '../../components/chat/SystemPromptEditor';

const PromptsSettings: React.FC = () => {
  const navigate = useNavigate();
  const [globalPrompts, setGlobalPrompts] = useAtom(globalPromptsAtom);

  const fetchGlobalPrompts = async () => {
    try {
      const response = await apiFetch('/api/systemPrompts');
      if (response.ok) {
        const data: PredefinedPrompt[] = await response.json();
        setGlobalPrompts(data);
      }
    } catch (error) {
      console.error('Error fetching global prompts:', error);
    }
  };

  useEffect(() => {
    document.title = 'Angel: System Prompts';
    fetchGlobalPrompts();
  }, []);

  const handleDeletePrompt = async (promptToDelete: PredefinedPrompt) => {
    if (window.confirm(`Are you sure you want to delete the prompt "${promptToDelete.label}"?`)) {
      const updatedPrompts = globalPrompts.filter((p) => p.label !== promptToDelete.label);
      const success = await savePromptsToBackend(updatedPrompts);
      if (success) {
        setGlobalPrompts(updatedPrompts);
        if (updatedPrompts.length === 0) {
          await fetchGlobalPrompts();
        }
      }
    }
  };

  const handleEditPrompt = (prompt: PredefinedPrompt) => {
    navigate(`/settings/prompts/${encodeURIComponent(prompt.label)}`);
  };

  const handleAddNewPrompt = () => {
    navigate('/settings/prompts/new');
  };

  const handleMoveUp = async (index: number) => {
    if (index === 0) return;
    const updatedPrompts = [...globalPrompts];
    [updatedPrompts[index], updatedPrompts[index - 1]] = [updatedPrompts[index - 1], updatedPrompts[index]];
    setGlobalPrompts(updatedPrompts);
    await savePromptsToBackend(updatedPrompts);
  };

  const handleMoveDown = async (index: number) => {
    if (index === globalPrompts.length - 1) return;
    const updatedPrompts = [...globalPrompts];
    [updatedPrompts[index], updatedPrompts[index + 1]] = [updatedPrompts[index + 1], updatedPrompts[index]];
    setGlobalPrompts(updatedPrompts);
    await savePromptsToBackend(updatedPrompts);
  };

  const savePromptsToBackend = async (prompts: PredefinedPrompt[]) => {
    try {
      const response = await apiFetch('/api/systemPrompts', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(prompts),
      });

      if (!response.ok) {
        alert('Failed to save prompts. Please try again.');
        return false;
      }
      return true;
    } catch (error) {
      alert('Error saving prompts. Please check your connection.');
      return false;
    }
  };

  // Render child route (PromptEditor) if present
  if (globalPrompts.length === 0) {
    return (
      <div>
        <h3>Global System Prompts</h3>
        <p>No global prompts defined. Add one below!</p>
        <button onClick={handleAddNewPrompt}>Add New Prompt</button>
        <Outlet />
      </div>
    );
  }

  return (
    <div>
      <h3>Global System Prompts</h3>
      <div style={{ border: '1px solid #eee', padding: '10px', minHeight: '100px' }}>
        <ul>
          {globalPrompts.map((prompt, index) => (
            <li key={prompt.label} style={{ marginBottom: '5px', display: 'flex', alignItems: 'center' }}>
              <strong>{prompt.label}:</strong> {prompt.value.substring(0, Math.min(prompt.value.length, 50))}
              ...
              <button onClick={() => handleEditPrompt(prompt)} style={{ marginLeft: '10px', padding: '2px 8px' }}>
                Edit
              </button>
              <button onClick={() => handleDeletePrompt(prompt)} style={{ marginLeft: '5px', padding: '2px 8px' }}>
                Delete
              </button>
              <button
                onClick={() => handleMoveUp(index)}
                disabled={index === 0}
                style={{ marginLeft: '5px', padding: '2px 8px' }}
              >
                Move Up
              </button>
              <button
                onClick={() => handleMoveDown(index)}
                disabled={index === globalPrompts.length - 1}
                style={{ marginLeft: '5px', padding: '2px 8px' }}
              >
                Move Down
              </button>
            </li>
          ))}
        </ul>
      </div>
      <div style={{ marginTop: '10px' }}>
        <button onClick={handleAddNewPrompt}>Add New Prompt</button>
      </div>
      <Outlet />
    </div>
  );
};

export default PromptsSettings;
