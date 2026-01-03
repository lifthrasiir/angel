import React, { useEffect, useState, useRef } from 'react';
import { useAtom } from 'jotai';

import { lastAutoDisplayedThoughtIdAtom } from '../../atoms/uiAtoms';
import type { ChatMessage } from '../../types/chat';
import ChatMessageComponent from './ChatMessage';
import { ProcessingIndicator } from './ProcessingIndicator';
import './ThoughtGroup.css';

interface ThoughtGroupProps {
  groupId: string; // Unique ID for this thought group
  isAutoDisplayMode: boolean;
  thoughts: ChatMessage[];
  isLastThoughtGroup?: boolean;
}

export const ThoughtGroup: React.FC<ThoughtGroupProps> = React.memo(
  ({ groupId, isAutoDisplayMode, thoughts, isLastThoughtGroup }) => {
    const [lastAutoDisplayedThoughtId] = useAtom(lastAutoDisplayedThoughtIdAtom);

    const [activeThoughtId, setActiveThoughtId] = useState<string | null>(null);
    // Track the previous latest thought ID to distinguish between auto-assigned and manual selection
    const previousLatestIdRef = useRef<string | null>(null);

    useEffect(() => {
      if (isAutoDisplayMode) {
        const latestThought = thoughts.find((thought) => thought.id === lastAutoDisplayedThoughtId);

        if (latestThought) {
          // This group does contain the thought to be automatically displayed.
          // There are three possible cases, assuming that lastAutoDisplayedThoughtId changed from A to B:
          // 1. activeThoughtId = null: User had nothing selected, so we auto-assign B
          // 2. activeThoughtId = A: User was viewing the previous latest thought, so we auto-switch to B
          // 3. activeThoughtId = C (C != A and C != B): User manually selected an older thought, so we keep C
          // 4. activeThoughtId = B is impossible because B has just arrived.
          if (activeThoughtId === previousLatestIdRef.current) {
            setActiveThoughtId(latestThought.id);
            previousLatestIdRef.current = latestThought.id;
          } else {
            // If user manually selected an older thought, keep their selection
          }
        } else if (lastAutoDisplayedThoughtId === null) {
          // Latest message is not a thought (e.g., model message), auto-close
          // Only close if user was viewing the latest thought (auto mode)
          if (activeThoughtId === previousLatestIdRef.current) {
            setActiveThoughtId(null);
            previousLatestIdRef.current = null;
          }
        }
      } else {
        setActiveThoughtId(null);
        previousLatestIdRef.current = null;
      }
    }, [isAutoDisplayMode, lastAutoDisplayedThoughtId, thoughts, groupId, activeThoughtId]);

    const handleCircleClick = (thought: ChatMessage) => {
      if (activeThoughtId === thought.id) {
        setActiveThoughtId(null);
      } else {
        setActiveThoughtId(thought.id);
      }
    };

    const getThoughtTitle = (thought: ChatMessage) => {
      if (!thought.parts || thought.parts.length === 0 || !thought.parts[0].text) return '';
      const lines = thought.parts[0].text.split('\n');
      const title = lines[0].trim();
      const content = lines.slice(1).join('\n').trim();
      return `${title}\n\n${content}`;
    };

    return (
      <div className="thought-group-container">
        <div className="thought-circle-container">
          {thoughts.map((thought) => (
            <button
              key={thought.id}
              id={thought.id}
              className={`thought-circle ${activeThoughtId === thought.id ? 'selected' : ''}`}
              onClick={() => handleCircleClick(thought)}
              title={getThoughtTitle(thought)}
              aria-label={`Thought: ${getThoughtTitle(thought).split('\n')[0]}`}
            ></button>
          ))}
          {isLastThoughtGroup && <ProcessingIndicator isLastThoughtGroup={true} isLastModelMessage={false} />}
        </div>
        {activeThoughtId !== null && thoughts.find((thought) => thought.id === activeThoughtId) && (
          <ChatMessageComponent
            key={activeThoughtId}
            message={thoughts.find((thought) => thought.id === activeThoughtId)!}
          />
        )}
      </div>
    );
  },
);
