# Resolution of Issue: Prepare next development iteration PR failure

## Issue Description
The automated pull request creation failed during the `workflow_dispatch` event with the following error:
`pull request create failed: GraphQL: Head sha can't be blank, Base sha can't be blank, No commits between main and bump-version-0.0.7-SNAPSHOT, Base ref must be a branch (createPullRequest)`

This issue occurred because the `gh pr create` command was defaulting to `main` as the base branch, either due to a hardcoded value or because `${{ github.event.repository.default_branch }}` was evaluating to an empty string on `workflow_dispatch` events. The repository's actual default branch is `master`.

## Resolution
This issue was previously identified and resolved in pull request #14 (commit `04e1099d5655ca27a39e0a32365d177d179b1d66`).

The fix involved updating the `.github/workflows/ci.yml` file to dynamically reference the base branch using `"${{ github.ref_name }}"` in all instances of `gh pr create`. This ensures that the automated pull requests correctly target the branch from which the workflow was triggered, preventing empty variable evaluation errors.

Since the fix was already merged into `master`, no further code changes are required for this specific issue. This document serves as confirmation that the issue has been successfully resolved.
