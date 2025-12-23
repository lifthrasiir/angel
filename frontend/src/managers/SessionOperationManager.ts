import type { FileAttachment } from '../types/chat';
import type { ModelInfo } from '../api/models';
import { apiFetch, fetchSessionHistory } from '../api/apiClient';
import { sendMessage, processStreamResponse, type SseEventHandler } from '../utils/messageHandler';
import { loadSession } from '../utils/sessionManager';
import type { SessionAction } from '../types/sessionFSM';
import {
  type SseEvent,
  EventComplete,
  EventError,
  parseSseEvent,
  EventInitialState,
  EventInitialStateNoCall,
  EARLIER_MESSAGES_LOADED,
} from '../types/events';

export interface MessageSendParams {
  content: string;
  attachments: FileAttachment[];
  model: ModelInfo | null;
  systemPrompt?: string;
  workspaceId?: string;
  primaryBranchId?: string;
  initialRoots?: string[];
  beforeMessageId?: string;
  isTemporary?: boolean;
}

export interface OperationEventHandlers {
  onInitialState?: (data: any) => void;
  onEvent?: (event: SseEvent) => void;
  onComplete?: () => void;
  onError?: (error: Error | Event | any) => void;
}

export class SessionOperationManager {
  private _dispatch: (action: SessionAction) => void;
  private activeOperation: 'none' | 'loading' | 'sending' | 'streaming' = 'none';
  private currentEventSource: EventSource | null = null;
  private currentAbortController: AbortController | null = null;
  private currentSessionId: string | null = null;
  private currentHandlers?: OperationEventHandlers;

  constructor(params: { dispatch: (action: SessionAction) => void; sessionState?: any }) {
    this._dispatch = params.dispatch;
  }

  /**
   * Get current active operation
   */
  getActiveOperation(): typeof this.activeOperation {
    return this.activeOperation;
  }

  /**
   * Cancel current operation if any
   */
  cancelCurrentOperation(): void {
    if (this.currentEventSource) {
      this.currentEventSource.close();
      this.currentEventSource = null;
    }

    if (this.currentAbortController) {
      this.currentAbortController.abort();
      this.currentAbortController = null;
    }

    this.activeOperation = 'none';
    this.currentSessionId = null;
    this.currentHandlers = undefined;
  }

  /**
   * Handle operation errors with consistent dispatch and cleanup
   */
  private handleOperationError(error: any, errorMessage: string): void {
    this._dispatch({ type: 'ERROR_OCCURRED', error: errorMessage });
    this.currentHandlers?.onError?.(error as Error);
    this.resetOperationState();
  }

  /**
   * Reset operation state consistently
   */
  private resetOperationState(): void {
    this.activeOperation = 'none';
    this.currentSessionId = null;
    this.currentAbortController = null;
    this.currentHandlers = undefined;
  }

  /**
   * Validate HTTP response and throw error if not ok
   */
  private validateResponse(response: Response, operationName: string): boolean {
    if (response.status === 401) {
      location.reload();
      return true;
    }

    if (!response.ok) {
      throw new Error(`${operationName} failed: ${response.status} ${response.statusText}`);
    }

    return false;
  }

  /**
   * Setup streaming operation with common initialization
   */
  private setupStreamingOperation(sessionId: string | null, handlers?: OperationEventHandlers): void {
    this.cancelCurrentOperation();
    this.activeOperation = 'sending';
    this.currentSessionId = sessionId;
    this.currentHandlers = handlers;
  }

  /**
   * Start streaming with common event handling
   */
  private async startStreaming(
    response: Response,
    sessionId: string | null,
    hasMoreMessages: boolean = false,
  ): Promise<void> {
    this.activeOperation = 'streaming';
    this.currentAbortController = new AbortController();

    const eventHandlers: SseEventHandler = (event) => {
      // Only process events if this is still the active operation
      if (this.activeOperation !== 'streaming' || this.currentSessionId !== sessionId) {
        return;
      }

      this.handleStreamingEvent(event, sessionId, hasMoreMessages);
    };

    await processStreamResponse(response, eventHandlers, this.currentAbortController.signal);

    // Cleanup on successful completion
    this.resetOperationState();
  }

  /**
   * Handle streaming events with consolidated switch statement
   */
  private handleStreamingEvent(event: SseEvent, sessionId: string | null, hasMoreMessages: boolean = false): void {
    switch (event.type) {
      case EventInitialState:
      case EventInitialStateNoCall: {
        const initialState = (event as any).initialState;

        // Dispatch SESSION_CREATED to update sessionManager.sessionId
        // This prevents duplicate session loads when URL is updated
        this._dispatch({
          type: 'SESSION_CREATED',
          sessionId: initialState.sessionId || sessionId,
          workspaceId: initialState.workspaceId,
        });

        const mappedData = this.mapInitialData(initialState, sessionId, {
          isCallActive: event.type === EventInitialState,
          hasMore: hasMoreMessages,
        });
        this.currentHandlers?.onInitialState?.(mappedData);
        break;
      }
      case EventComplete:
        this._dispatch({ type: 'STREAM_COMPLETED', activeOperation: 'none' });
        this.currentHandlers?.onComplete?.();
        this.activeOperation = 'none';
        break;
      case EventError:
        this._dispatch({ type: 'ERROR_OCCURRED', error: (event as any).error || 'Unknown error' });
        this.currentHandlers?.onError?.(event);
        this.activeOperation = 'none';
        break;
      default:
        this.currentHandlers?.onEvent?.(event);
    }
  }

  /**
   * Map initial state data to unified format
   */
  private mapInitialData(
    initialState: any,
    sessionId: string | null,
    options: {
      isCallActive: boolean;
      hasMore?: boolean;
      fetchLimit?: number;
    },
  ) {
    // If fetchLimit is provided, calculate hasMore from message count
    const hasMore =
      options.hasMore !== undefined
        ? options.hasMore
        : options.fetchLimit !== undefined && (initialState.history || []).length > options.fetchLimit;

    return {
      isCallActive: options.isCallActive,
      sessionId: initialState.sessionId || sessionId,
      messages: initialState.history || [],
      systemPrompt: initialState.systemPrompt,
      primaryBranchId: initialState.primaryBranchId,
      hasMore,
      elapsedTimeMs:
        options.isCallActive && initialState.callElapsedTimeSeconds
          ? initialState.callElapsedTimeSeconds * 1000
          : undefined,
      pendingConfirmation: initialState.pendingConfirmation,
      workspaceId: initialState.workspaceId,
    };
  }

  /**
   * Handle session loading operation
   */
  async handleSessionLoad(
    sessionId: string,
    fetchLimit: number = 50,
    handlers?: OperationEventHandlers,
  ): Promise<void> {
    this.cancelCurrentOperation();

    this.activeOperation = 'loading';
    this.currentSessionId = sessionId;
    this.currentHandlers = handlers;

    try {
      this.currentEventSource = loadSession(
        sessionId,
        fetchLimit,
        (event: MessageEvent) => {
          if (this.activeOperation !== 'loading' || this.currentSessionId !== sessionId) {
            return;
          }

          try {
            const parsedEvent = parseSseEvent(event.data);

            switch (parsedEvent.type) {
              case EventInitialState:
              case EventInitialStateNoCall: {
                const initialState = (parsedEvent as any).initialState;
                console.log('EventInitialState data:', initialState);

                const mappedData = this.mapInitialData(initialState, sessionId, {
                  isCallActive: event.type === EventInitialState,
                  fetchLimit,
                });

                this._dispatch({
                  type: 'SESSION_LOADED',
                  sessionId,
                  workspaceId: initialState.workspaceId,
                  hasEarlier: mappedData.hasMore,
                  activeOperation: 'none',
                });

                handlers?.onInitialState?.(mappedData);

                if (parsedEvent.type === EventInitialStateNoCall) {
                  // Close the EventSource since this is the final event
                  if (this.currentEventSource) {
                    this.currentEventSource.close();
                    this.currentEventSource = null;
                  }
                  // Reset active operation since session load is complete
                  this.resetOperationState();
                }
                break;
              }
              default:
                handlers?.onEvent?.(parsedEvent);
            }
          } catch (error) {
            console.error('Failed to parse SSE event:', error);
            handlers?.onError?.(error as Error);
          }
        },
        (error: Event) => {
          if (this.activeOperation === 'loading' && this.currentSessionId === sessionId) {
            this._dispatch({ type: 'ERROR_OCCURRED', error: 'Failed to load session' });
            handlers?.onError?.(error);
          }
        },
      );
    } catch (error) {
      this.handleOperationError(error, `Session load failed: ${error}`);
    }
  }

  /**
   * Handle message sending operation
   */
  async handleMessageSend(
    params: MessageSendParams,
    sessionId: string | null,
    handlers?: OperationEventHandlers,
  ): Promise<void> {
    this.setupStreamingOperation(sessionId, handlers);

    try {
      this._dispatch({ type: 'STREAM_STARTED', activeOperation: 'streaming' });

      const response = await sendMessage(
        params.content,
        params.attachments,
        sessionId,
        params.systemPrompt || '',
        params.workspaceId,
        params.primaryBranchId,
        params.model?.name,
        params.initialRoots,
        params.beforeMessageId,
        params.isTemporary,
      );

      if (this.validateResponse(response, 'Send')) return;

      await this.startStreaming(response, sessionId, false);
    } catch (error) {
      this.handleOperationError(error, `Message send failed: ${error}`);
    }
  }

  /**
   * Handle branch switching operation
   */
  async handleBranchSwitch(sessionId: string, branchId: string): Promise<void> {
    this.cancelCurrentOperation();

    this.activeOperation = 'sending';
    this.currentSessionId = sessionId;

    try {
      const response = await apiFetch(`/api/chat/${sessionId}/branch/${branchId}`, {
        method: 'POST',
      });

      if (this.validateResponse(response, 'Branch switch')) return;

      // Load session after branch switch
      await this.handleSessionLoad(sessionId);
    } catch (error) {
      this.handleOperationError(error, `Branch switch failed: ${error}`);
    }
  }

  /**
   * Handle tool confirmation operation
   */
  async handleToolConfirmation(
    sessionId: string,
    branchId: string,
    modifiedData?: Record<string, any>,
    handlers?: OperationEventHandlers,
  ): Promise<void> {
    this.setupStreamingOperation(sessionId, handlers);

    try {
      const requestBody: { approved: boolean; modifiedData?: Record<string, any> } = { approved: true };
      if (modifiedData) {
        requestBody.modifiedData = modifiedData;
      }

      const response = await apiFetch(`/api/chat/${sessionId}/branch/${branchId}/confirm`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
      });

      if (this.validateResponse(response, 'Tool confirmation')) return;

      await this.startStreaming(response, sessionId, false);
    } catch (error) {
      this.handleOperationError(error, `Tool confirmation failed: ${error}`);
    }
  }

  /**
   * Handle earlier messages loading
   */
  async handleEarlierMessagesLoad(
    sessionId: string,
    beforeMessageId: string,
    fetchLimit: number = 50,
    handlers?: OperationEventHandlers,
  ): Promise<void> {
    if (this.activeOperation !== 'none') {
      console.warn('Cannot load earlier messages while another operation is active');
      return;
    }

    this.activeOperation = 'loading';

    try {
      this._dispatch({ type: 'EARLIER_MESSAGES_LOADING' });

      const data = await fetchSessionHistory(sessionId, beforeMessageId, fetchLimit);

      const messageCount = (data.history || []).length;
      const hasMore = messageCount > fetchLimit;

      this._dispatch({
        type: 'EARLIER_MESSAGES_LOADED',
        hasMore: hasMore,
      });

      handlers?.onEvent?.({
        type: EARLIER_MESSAGES_LOADED,
        data: {
          ...data,
          hasMore: hasMore,
        },
      });
    } catch (error) {
      this._dispatch({ type: 'ERROR_OCCURRED', error: `Earlier messages load failed: ${error}` });
      handlers?.onError?.(error as Error);
    } finally {
      this.activeOperation = 'none';
    }
  }

  /**
   * Handle message retry operation
   */
  async handleMessageRetry(
    sessionId: string,
    originalMessageId: string,
    content: string,
    model: ModelInfo | null,
    systemPrompt?: string,
    handlers?: OperationEventHandlers,
  ): Promise<void> {
    this.setupStreamingOperation(sessionId, handlers);

    try {
      const requestBody: any = {
        content,
        model: model?.name || null,
        systemPrompt: systemPrompt || '',
        retry: 1,
        originalMessageId,
      };

      const response = await apiFetch(`/api/chat/${sessionId}/branch?retry=1`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
      });

      if (this.validateResponse(response, 'Message retry')) return;

      await this.startStreaming(response, sessionId, false);
    } catch (error) {
      this.handleOperationError(error, `Message retry failed: ${error}`);
    }
  }

  /**
   * Handle message edit operation
   */
  async handleMessageEdit(
    sessionId: string,
    originalMessageId: string,
    editedText: string,
    model: ModelInfo | null,
    systemPrompt?: string,
    handlers?: OperationEventHandlers,
  ): Promise<void> {
    this.setupStreamingOperation(sessionId, handlers);

    try {
      const requestBody = {
        updatedMessageId: parseInt(originalMessageId, 10),
        newMessageText: editedText,
        model: model?.name || null,
        systemPrompt: systemPrompt || '',
      };

      const response = await apiFetch(`/api/chat/${sessionId}/branch`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
      });

      if (this.validateResponse(response, 'Message edit')) return;

      await this.startStreaming(response, sessionId, false);
    } catch (error) {
      this.handleOperationError(error, `Message edit failed: ${error}`);
    }
  }

  /**
   * Handle error retry operation
   */
  async handleErrorRetry(sessionId: string, branchId: string, handlers?: OperationEventHandlers): Promise<void> {
    this.setupStreamingOperation(sessionId, handlers);

    try {
      const response = await apiFetch(`/api/chat/${sessionId}/branch/${branchId}/retry-error`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      });

      if (this.validateResponse(response, 'Error retry')) return;

      await this.startStreaming(response, sessionId, false);
    } catch (error) {
      this.handleOperationError(error, `Error retry failed: ${error}`);
    }
  }
}
