---
description: Stage all changes and create a commit
model: opus
allowed-tools: Bash(git add:*), Bash(git status:*), Bash(git diff:*), Bash(git commit:*)
---
## Context
- Status: !`git status --short`
- Staged diff: !`git diff --cached`
- Unstaged diff: !`git diff`

## Task
Review all changes above. Stage everything that isn't staged yet (`git add -A`),
write a conventional commit message, and commit. Split unrelated changes into
separate commits when it makes sense.
