import { useMemo } from 'react';
import type { ChatMessage as ChatMessageType } from '../types/chat';
import { ProcessingIndicator } from '../components/chat/ProcessingIndicator';
import ChatMessageComponent from '../components/chat/ChatMessage';
import { ThoughtGroup } from '../components/chat/ThoughtGroup';
import FunctionPairMessage from '../components/chat/FunctionPairMessage';
import MessageInfo from '../components/chat/MessageInfo';

interface UseMessageGroupingParams {
  messages: ChatMessageType[];
  availableModels: Map<string, { maxTokens: number }>;
  startTime: number | undefined;
  temporaryEnvChangeMessage: ChatMessageType | null;
  handleEditMessage: (originalMessageId: string, editedText: string) => Promise<void>;
  handleBranchSwitch: (newBranchId: string) => Promise<void>;
  handleRetryMessage?: (originalMessageId: string) => Promise<void>;
  handleRetryError?: (errorMessageId: string) => Promise<void>;
  handleUpdateMessage?: (messageId: string, editedText: string) => Promise<void>;
  handleContinueMessage?: (messageId: string) => Promise<void>;
}

interface UseMessageGroupingResult {
  renderedMessages: JSX.Element[];
  mostRecentUserMessageId: string | null;
}

export const useMessageGrouping = ({
  messages,
  availableModels,
  startTime,
  temporaryEnvChangeMessage,
  handleEditMessage,
  handleBranchSwitch,
  handleRetryMessage,
  handleRetryError,
  handleUpdateMessage,
  handleContinueMessage,
}: UseMessageGroupingParams): UseMessageGroupingResult => {
  // Helper function to check if a message is retryable (last consecutive error messages)
  const isRetryableError = (message: ChatMessageType, allMessages: ChatMessageType[]): boolean => {
    if (message.type !== 'model_error') return false;

    // Check if this message is part of the last consecutive error messages
    for (let i = allMessages.length - 1; i >= 0; i--) {
      const msg = allMessages[i];
      if (msg.type === 'model_error') {
        if (msg.id === message.id) {
          // Found the message in the consecutive error block
          return true;
        }
      } else {
        // Found non-error message, stop
        break;
      }
    }

    return false;
  };

  const renderMessageOrGroup = (
    currentMessage: ChatMessageType,
    messages: ChatMessageType[],
    currentIndex: number,
    availableModels: Map<string, { maxTokens: number }>,
    handleEditMessage: (originalMessageId: string, editedText: string) => Promise<void>,
    handleBranchSwitch: (newBranchId: string) => Promise<void>,
    handleRetryError?: (errorMessageId: string) => Promise<void>,
    handleUpdateMessage?: (messageId: string, editedText: string) => Promise<void>,
    handleContinueMessage?: (messageId: string) => Promise<void>,
    mostRecentUserMessageId?: string | null,
  ): { element: JSX.Element; messagesConsumed: number } => {
    // Find maxTokens for the current message's model
    const currentModelMaxTokens = currentMessage.model
      ? availableModels.get(currentMessage.model)?.maxTokens
      : undefined;

    const isLastMessage = currentIndex === messages.length - 1;

    // Check for function_call followed by function_response
    if (
      currentMessage.type === 'function_call' &&
      currentMessage.parts &&
      currentMessage.parts.length > 0 &&
      currentMessage.parts[0].functionCall &&
      currentIndex + 1 < messages.length &&
      messages[currentIndex + 1].type === 'function_response' &&
      messages[currentIndex + 1].parts &&
      messages[currentIndex + 1].parts.length > 0 &&
      messages[currentIndex + 1].parts[0].functionResponse
    ) {
      const functionCall = currentMessage.parts[0].functionCall!;
      const functionResponse = messages[currentIndex + 1].parts[0].functionResponse!;

      // Create messageInfo for the function call message
      const callMessageInfoComponent = (
        <MessageInfo
          cumulTokenCount={currentMessage.cumulTokenCount}
          possibleBranches={currentMessage.possibleBranches}
          model={currentMessage.model}
          maxTokens={availableModels.get(currentMessage.model || '')?.maxTokens}
          sessionId={currentMessage.sessionId}
        />
      );

      // Create messageInfo for the function response message
      const responseMessage = messages[currentIndex + 1];
      const responseMessageInfoComponent = (
        <MessageInfo
          cumulTokenCount={responseMessage.cumulTokenCount}
          possibleBranches={responseMessage.possibleBranches}
          model={responseMessage.model}
          maxTokens={availableModels.get(responseMessage.model || '')?.maxTokens}
          sessionId={responseMessage.sessionId}
        />
      );

      return {
        element: (
          <>
            <FunctionPairMessage
              key={`function-pair-${currentMessage.id}-${messages[currentIndex + 1].id}`}
              functionCall={functionCall}
              functionResponse={functionResponse}
              callMessageId={currentMessage.id}
              responseMessageId={messages[currentIndex + 1].id}
              callMessageInfo={callMessageInfoComponent}
              responseMessageInfo={responseMessageInfoComponent}
              responseAttachments={messages[currentIndex + 1].attachments}
              sessionId={currentMessage.sessionId}
            />
            {isLastMessage && <ProcessingIndicator isLastThoughtGroup={false} isLastModelMessage={true} />}
          </>
        ),
        messagesConsumed: 2,
      };
    } else if (currentMessage.type === 'thought') {
      const thoughtGroup: ChatMessageType[] = [];
      let j = currentIndex;
      while (j < messages.length && messages[j].type === 'thought') {
        thoughtGroup.push(messages[j]);
        j++;
      }

      // Check if this thought group is the very last message(s)
      const isLastThoughtGroup = j === messages.length;

      return {
        element: (
          <ThoughtGroup
            key={`thought-group-${currentIndex}`}
            groupId={`thought-group-${currentIndex}`}
            isAutoDisplayMode={true}
            thoughts={thoughtGroup}
            isLastThoughtGroup={isLastThoughtGroup}
          />
        ),
        messagesConsumed: j - currentIndex,
      };
    } else {
      const isLastModelMessage =
        isLastMessage && (['model', 'model_error', 'error'] as (string | undefined)[]).includes(currentMessage.type);
      return {
        element: (
          <>
            <ChatMessageComponent
              key={currentMessage.id}
              message={currentMessage}
              maxTokens={currentModelMaxTokens}
              isLastModelMessage={isLastModelMessage}
              onSaveEdit={handleEditMessage}
              onRetryClick={handleRetryMessage ? (messageId) => handleRetryMessage(messageId) : undefined}
              onRetryError={
                handleRetryError && isRetryableError(currentMessage, messages)
                  ? (errorMessageId) => handleRetryError(errorMessageId)
                  : undefined
              }
              onBranchSelect={handleBranchSwitch}
              onSaveUpdate={handleUpdateMessage}
              onContinueClick={handleContinueMessage}
              isMostRecentUserMessage={currentMessage.id === mostRecentUserMessageId}
            />
          </>
        ),
        messagesConsumed: 1,
      };
    }
  };

  // Logic to group consecutive thought messages
  const renderedMessages = useMemo(() => {
    const renderedElements: JSX.Element[] = [];
    let i = 0; // This will become our startIndex

    // Find the most recent user message ID for accesskey shortcuts
    let mostRecentUserMessageId: string | null = null;
    for (let j = messages.length - 1; j >= 0; j--) {
      if (messages[j].type === 'user') {
        mostRecentUserMessageId = messages[j].id;
        break;
      }
    }

    // Calculate the starting index, skipping incomplete groups at the beginning
    while (i < messages.length) {
      const currentMessage = messages[i];

      // Case 1: Incomplete FunctionPairMessage (function_response without preceding function_call)
      if (currentMessage.type === 'function_response') {
        i++; // Skip this message
        continue;
      }

      // Case 2: Incomplete ThoughtGroup (thought message that's a continuation of a group not fully loaded)
      // If the first message is a thought, we assume it's incomplete and skip it.
      if (currentMessage.type === 'thought') {
        i++; // Skip this message
        continue;
      }

      // If we reach here, the current message is either a complete group or a standalone message
      // that can be rendered, or it's a thought message that's not at the very beginning (meaning it's part of a group that started within the current view).
      // So, we break the loop and start rendering from this 'i'.
      break;
    }

    // Now, render messages from the calculated startIndex
    while (i < messages.length) {
      const currentMessage = messages[i];
      const { element, messagesConsumed } = renderMessageOrGroup(
        currentMessage,
        messages,
        i,
        availableModels,
        handleEditMessage,
        handleBranchSwitch,
        handleRetryError,
        handleUpdateMessage,
        handleContinueMessage,
        mostRecentUserMessageId,
      );
      renderedElements.push(element);
      i += messagesConsumed;
    }

    // Add temporary environment change message if it exists
    if (temporaryEnvChangeMessage) {
      renderedElements.push(
        <ChatMessageComponent
          key={temporaryEnvChangeMessage.id}
          message={temporaryEnvChangeMessage}
          maxTokens={undefined} // Temporary messages don't have token limits
          isLastModelMessage={false}
          onSaveEdit={() => {}}
          onRetryClick={handleRetryMessage ? (messageId) => handleRetryMessage(messageId) : undefined}
          onRetryError={handleRetryError ? (errorMessageId) => handleRetryError(errorMessageId) : undefined}
          onBranchSelect={handleBranchSwitch}
          isMostRecentUserMessage={false}
        />,
      );
    }

    // Check if we need to render ProcessingIndicator outside message list
    const shouldRenderOutsideProcessingIndicator = () => {
      if (!startTime) return false;

      // If there are no messages, render outside
      if (messages.length === 0) return true;

      // Check if the last message is a model message or thought
      const lastMessage = messages[messages.length - 1];
      return (
        !lastMessage.type || !(['model', 'model_error', 'error', 'thought'] as string[]).includes(lastMessage.type)
      );
    };

    // Add ProcessingIndicator outside message list when needed
    if (shouldRenderOutsideProcessingIndicator()) {
      renderedElements.push(
        <ProcessingIndicator key="processing-indicator" isLastThoughtGroup={false} isLastModelMessage={false} />,
      );
    }

    return renderedElements;
  }, [
    messages,
    availableModels,
    startTime,
    temporaryEnvChangeMessage,
    handleEditMessage,
    handleBranchSwitch,
    handleRetryMessage,
    handleRetryError,
    handleUpdateMessage,
    handleContinueMessage,
  ]);

  // Find most recent user message ID
  const mostRecentUserMessageId = useMemo(() => {
    for (let j = messages.length - 1; j >= 0; j--) {
      if (messages[j].type === 'user') {
        return messages[j].id;
      }
    }
    return null;
  }, [messages]);

  return { renderedMessages, mostRecentUserMessageId };
};
