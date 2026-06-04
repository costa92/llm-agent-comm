// Package a2a is a simplified Agent-to-Agent protocol over HTTP.
//
// What's covered:
//
//   - Server: registers skills, exposes /skills + POST /tasks
//     + GET /tasks/{id} + DELETE /tasks/{id} (cancel)
//   - Task state machine: pending → running → completed/failed
//     (cancel reuses TaskFailed with Error="canceled by DELETE")
//   - Client: ListSkills + ExecuteSkill (POST + poll until artifact)
//   - AsAgentTool: wrap a remote skill as agents.Tool
//
// What's NOT covered:
//
//   - Wire compatibility with Google's a2a-sdk (custom-and-tiny schema)
//   - Pause / resume on tasks (cancel via DELETE; no pause/resume yet)
//   - Client-side DeleteTask helper (server-side cancel only for now)
//   - Auth / encryption / signed messages
//   - Long-running task persistence (in-memory only; restart loses state)
//
// # Portability
//
// a2a inherits the agents/pkg/llm portability contract.
package a2a
