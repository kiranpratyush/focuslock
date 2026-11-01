// Package judge answers one question: what is this window for, given the
// task? Rules can't decide everything ("Go concurrency talk" vs "funny
// video" are both youtube.com/watch), so the classifier asks a judge.
//
// A judge only classifies. It gets a Question (app, title, page, task)
// and returns an Answer (a category with probabilities). Turning that into
// allow or block is Decide's job (verdict.go), unless a judge knows its own
// answers better and implements Verdict (jevos does).
//
// The judges, one file each, register themselves by name in init():
//
//	jev.go      TypeSafe Jev, a classifier API with real probabilities
//	openai.go   any OpenAI-compatible chat API (OpenAI, Groq, Ollama, LM Studio...)
//	copilot.go  GitHub Copilot through the Copilot CLI, with all tools off
//	jevos.go    jevos, an open-source Jev on your own CPU; asks yes/no questions
//
// Adding a judge means one new file that implements Judge and calls
// registerJudge. Optional extras are small interfaces a judge may also
// implement (judge.go): its own timeout, Close, its own Verdict, and
// Describe (spelling out the task, describe.go).
package judge
