import React from 'react';
import { FunctionCall, FunctionResponse } from '../types/chat';

interface FunctionCallMessageProps {
  functionCall: FunctionCall;
  messageId?: string;
}

interface FunctionResponseMessageProps {
  functionResponse: FunctionResponse;
  messageId?: string;
}

type FunctionCallComponent = React.ComponentType<FunctionCallMessageProps>;
type FunctionResponseComponent = React.ComponentType<FunctionResponseMessageProps>;

interface FunctionMessageRegistry {
  callComponents: Map<string, FunctionCallComponent>;
  responseComponents: Map<string, FunctionResponseComponent>;
}

const registry: FunctionMessageRegistry = {
  callComponents: new Map(),
  responseComponents: new Map(),
};

export const registerFunctionCallComponent = (functionName: string, component: FunctionCallComponent) => {
  registry.callComponents.set(functionName, component);
};

export const registerFunctionResponseComponent = (functionName: string, component: FunctionResponseComponent) => {
  registry.responseComponents.set(functionName, component);
};

export const getFunctionCallComponent = (functionName: string) => {
  return registry.callComponents.get(functionName);
};

export const getFunctionResponseComponent = (functionName: string) => {
  return registry.responseComponents.get(functionName);
};
