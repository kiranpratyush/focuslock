// Package classify decides what to do with the window in front of you.
//
// Classifier.Classify goes from cheap and certain to slow and fuzzy, and
// stops at the first answer:
//
//  1. Address rules (urls.go, rules.go): allowed sites, search pages,
//     music sites, blocked sites.
//  2. Process and title rules (rules.go): blocked apps, always-allowed apps,
//     title keywords. Browser pop-ups aren't judged at all (titles.go).
//  3. Explanations accepted earlier this session (explain.go).
//  4. Strict sites (YouTube, X...): a placeholder title like "YouTube - Brave"
//     is blocked outright (titles.go).
//  5. The judge (package judge), once the title has stayed the same for
//     a moment. The answer is cached, and Classify never waits for it: until
//     it arrives, the window counts as pending.
//
// Also here: explaining a block (explain.go), check windows for email and
// chat (ration.go), and judge-eval, which scores a judge on labelled cases
// from your log (eval.go).
package classify
