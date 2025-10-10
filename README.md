# 😇 Angel

An experimental web-based personal LLM agent based on [gemini-cli]. Your mileage may vary.

[gemini-cli]: https://github.com/google-gemini/gemini-cli/

## Features

* Model selection between Gemini 2.5 Flash and Pro among others
  - Leverages the free tier available through Gemini Code Assist via Google
* Multiple sessions and workspaces
  - Configurable system prompt per session
  - Automatic session name inference
* Conversation branching and editing
* Thought display
* File upload, either by drag-and-drop or file picker
* Context compression (`/compress`)
* Tool execution with MCP support and confirmation
  - Rudimentary filesystem tools with configurable roots (`/expose`, `/unexpose`) and syntax highlighting & diffing
  - Rudimentary shell command tools, polling supported for long-running commands
  - Web fetch tool with text extraction and summarization
  - Subagent and nanobanana-powered image generation support
  - Dedicated to-do tool
* Responsive UI
* 100% vibe-coded because why not
