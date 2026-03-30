---
name: pr
description: Create a PR and self-review for clarity, simplification, and correctness.
---

1. `git log --oneline main..HEAD` and `git diff main...HEAD --stat` to scope the changeset.
2. `git push -u origin HEAD` if not already pushed.
3. `gh pr create` with a short title and a body: `## Summary` (1-3 bullets), `## Test plan` (checklist).
4. Print the PR URL.
5. Spawn a review subagent. It reads `git diff main...HEAD` and posts one `gh pr review` comment covering:
   - **Clarity** — anything a reader must re-read to understand.
   - **Simplification** — dead code, unnecessary abstraction, roundabout logic.
   - **Correctness** — bugs, races, missing error checks, silent failures.
   If the diff is clean, post a short approval instead of inventing issues. No style nits.
6. If findings warrant changes, address them, push a revision, then `gh pr merge`. If clean, merge immediately.
