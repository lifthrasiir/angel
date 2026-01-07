import type React from 'react';
import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useAtom } from 'jotai';
import { apiFetch } from '../../api/apiClient';
import { globalPromptsAtom } from '../../atoms/modelAtoms';
import SystemPromptEditor from '../../components/chat/SystemPromptEditor';
import type { PredefinedPrompt } from '../../components/chat/SystemPromptEditor';

interface PromptEditorProps {
  isNew?: boolean;
}

const PromptEditor: React.FC<PromptEditorProps> = ({ isNew: isNewProp }) => {
  const { promptLabel } = useParams<{ promptLabel?: string }>();
  const navigate = useNavigate();
  const [globalPrompts, setGlobalPrompts] = useAtom(globalPromptsAtom);

  const [editingPrompt, setEditingPrompt] = useState<PredefinedPrompt | null>(null);
  const [newPromptLabel, setNewPromptLabel] = useState('');
  const [newPromptValue, setNewPromptValue] = useState('');

  // Determine if we're adding a new prompt or editing an existing one
  const isAddingNewPrompt = promptLabel === 'new' || isNewProp;
  const isEditingPrompt = !isAddingNewPrompt && promptLabel !== undefined;

  // Load prompt data when editing based on URL
  useEffect(() => {
    if (isEditingPrompt && globalPrompts.length > 0) {
      const decodedLabel = decodeURIComponent(promptLabel!);
      const promptToEdit = globalPrompts.find((p) => p.label === decodedLabel);
      if (promptToEdit) {
        setEditingPrompt(promptToEdit);
        setNewPromptLabel(promptToEdit.label);
        setNewPromptValue(promptToEdit.value);
      } else {
        // Prompt not found, redirect to prompts list
        navigate('/settings/prompts', { replace: true });
      }
    }
  }, [promptLabel, globalPrompts, isEditingPrompt, navigate]);

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

  const handleSavePrompt = async () => {
    let updatedPrompts: PredefinedPrompt[] = [];

    if (isAddingNewPrompt) {
      if (newPromptLabel.trim() === '' || newPromptValue.trim() === '') {
        alert('Label and prompt content cannot be empty.');
        return;
      }
      // Check for duplicate label
      if (globalPrompts.some((p) => p.label === newPromptLabel)) {
        alert('A prompt with this label already exists. Please use a unique label.');
        return;
      }
      const newPrompt: PredefinedPrompt = { label: newPromptLabel, value: newPromptValue };
      updatedPrompts = [...globalPrompts, newPrompt];
    } else if (editingPrompt) {
      if (newPromptLabel.trim() === '' || newPromptValue.trim() === '') {
        alert('Label and prompt content cannot be empty.');
        return;
      }
      // Check for duplicate label, excluding the current editing prompt
      if (globalPrompts.some((p) => p.label === newPromptLabel && p.label !== editingPrompt.label)) {
        alert('A prompt with this label already exists. Please use a unique label.');
        return;
      }
      updatedPrompts = globalPrompts.map((p) =>
        p.label === editingPrompt.label ? { label: newPromptLabel, value: newPromptValue } : p,
      );
    }

    const success = await savePromptsToBackend(updatedPrompts);
    if (success) {
      setGlobalPrompts(updatedPrompts);
      navigate('/settings/prompts');
    }
  };

  const handleCancelEdit = () => {
    navigate('/settings/prompts');
  };

  return (
    <div>
      <h4>{isAddingNewPrompt ? 'Add New Prompt' : 'Edit Prompt'}</h4>
      <div style={{ border: '1px solid #eee', padding: '10px' }}>
        <SystemPromptEditor
          initialPrompt={newPromptValue}
          currentLabel={newPromptLabel}
          onPromptUpdate={(updatedPrompt) => {
            setNewPromptLabel(updatedPrompt.label);
            setNewPromptValue(updatedPrompt.value);
          }}
          isEditing={true}
          isGlobalSettings={true}
        />
      </div>
      <div style={{ marginTop: '10px' }}>
        <button onClick={handleSavePrompt} style={{ marginRight: '10px' }}>
          Save
        </button>
        <button onClick={handleCancelEdit}>Cancel</button>
      </div>
    </div>
  );
};

export default PromptEditor;
