// Package session is the running focus session as saved on disk
// (state.json): when it ends, the task, this session's extra sites and
// apps, and the explanations accepted so far.
//
// A session belongs to one Windows boot (BootTime), so restarting the PC
// ends it. The worker reads it at start, so a worker that gets killed and
// restarted picks up where it left off.
package session
