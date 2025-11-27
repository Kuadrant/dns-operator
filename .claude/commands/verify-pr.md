You are analyzing whether the code changes in the current branch adequately address the requirements specified in a GitHub issue. This verification helps ensure PRs are complete before submission.

**Parameters:**
- Target branch: $1 (e.g., origin/main, main)
- Issue URL: $2 (e.g., https://github.com/Kuadrant/dns-operator/issues/627)

## Step 1: Validate Parameters and Extract Issue Information

First, validate the parameters:
- Ensure both target branch and issue URL are provided
- Extract the issue number and repository from the URL (format: https://github.com/{owner}/{repo}/issues/{number})
- If parameters are missing or invalid, provide clear error messages

## Step 2: Gather Issue Details

Use a two-tier approach to fetch issue information, preferring `gh` CLI but gracefully falling back to WebFetch:

**Tier 1: Try `gh` CLI first (if available):**
```bash
# Check if gh is available
which gh

# If available, fetch issue details
gh issue view {issue-number} --repo {owner}/{repo} --json title,body,labels,state,assignees,comments
```

Benefits of `gh` CLI:
- Provides structured metadata (labels, state, assignees)
- Can fetch comments for additional context
- Formatted JSON output for parsing

**Tier 2: Fallback to WebFetch (always works):**

If `gh` is not installed or fails (authentication, network issues), use the WebFetch tool:
```
WebFetch(url: {issue-url}, prompt: "Extract the issue title, description, requirements, and acceptance criteria...")
```

Benefits of WebFetch:
- No installation required
- Works without authentication
- No external dependencies

**Note in output which method was used** (e.g., "📡 Issue fetched via: gh CLI" or "📡 Issue fetched via: WebFetch")

From the issue, extract:
- Title and main description
- Stated requirements and acceptance criteria
- Any task lists or checkboxes
- Key discussion points from comments that clarify requirements (if available)
- Relevant labels (bug, enhancement, documentation, etc.) (if available)

## Step 3: Gather Code Changes

Collect all changes between the target branch and current HEAD:

1. **Diff analysis:**
```bash
git diff {target-branch}...HEAD
```

2. **Commit history:**
```bash
git log {target-branch}..HEAD --oneline
```

3. **File statistics:**
```bash
git diff {target-branch}...HEAD --stat
```

Read the full content of key modified files to understand context beyond just the diff.

## Step 4: Perform AI Analysis

Analyze the relationship between issue requirements and code changes:

### A. Requirement Extraction
Parse the issue to identify:
- Explicit requirements (numbered lists, acceptance criteria sections)
- Implicit requirements (problems described that need solutions)
- Expected behavior changes
- Documentation or test expectations

### B. Change Categorization
Organize code changes by:
- **Feature implementation**: New functionality added
- **Bug fixes**: Problems resolved
- **Tests**: New or updated tests
- **Documentation**: README, code comments, API docs
- **Refactoring**: Code improvements without behavior changes
- **Configuration**: Config files, dependencies, build changes

### C. Coverage Mapping
For each requirement from the issue:
- ✅ **Fully Addressed**: Cite specific code changes (file:line) that implement this requirement
- ⚠️ **Partially Addressed**: Identify what's done and what's missing
- ❌ **Not Addressed**: Flag missing implementations

### D. Quality Assessment
Evaluate:
- Are there tests covering the new/changed functionality?
- Is documentation updated to reflect changes?
- Do commit messages reference the issue?
- Are there any obvious bugs or issues in the implementation?
- Does the code follow existing patterns in the codebase?

## Step 5: Generate Interactive Review

Provide a structured report with these sections:

### 🎯 TL;DR

Provide a quick executive summary at the very top:
```
**Status:** ✅ Ready for PR | ⚠️ Mostly Ready | ❌ Needs Work
**Confidence:** High (85-100%) | Medium (60-84%) | Low (<60%)
**Blockers:** None | {number} critical issues
**Key Recommendations:** {number} high priority, {number} medium priority, {number} low priority

**Quick Stats:**
- ✅ {X}/{Y} requirements fully addressed
- ⚠️ {X}/{Y} requirements partially addressed
- ❌ {X}/{Y} requirements not addressed
- ✅ {passed} tests passing, {failed} failing
- 📝 {N} files changed (+{insertions}/-{deletions})
- 🧪 {N} new tests added (or ⚠️ 0 new tests added)
```

### 📋 Issue Summary
- Issue #{number}: {title}
- 📡 Issue fetched via: {gh CLI | WebFetch}
- Type: {bug/enhancement/etc based on labels if available}
- Key requirements (bulleted list)

### 📊 Changes Overview
- X files changed, Y insertions(+), Z deletions(-)
- N commits
- Key files modified (with file paths as clickable references)

### ✅ Requirement Coverage Analysis

For each requirement, provide:
```
**Requirement: {requirement description}**
Status: ✅ Fully Addressed | ⚠️ Partially Addressed | ❌ Not Addressed

Evidence:
- {specific code references with file:line}
- {relevant commit messages}

[If partially/not addressed]
Gap: {what's missing}
```

### 🔍 Code Quality Observations

Highlight:
- Test coverage (are there tests? do they cover the changes?)
- Documentation updates (README, comments, API docs)
- Consistency with codebase patterns
- Potential issues or concerns

### 💡 Suggestions

Provide actionable recommendations:
```
Priority: High | Medium | Low
- {specific suggestion with reasoning}
- {file references where changes should be made}
```

### 🎯 Final Verdict

Provide a clear assessment:
- **✅ Ready for PR**: All requirements addressed, good test coverage, docs updated
- **⚠️ Mostly Ready**: Core requirements met but some improvements recommended
- **❌ Needs Work**: Critical requirements missing or significant gaps

Include a brief summary justification.

## Error Handling

If you encounter any of these situations:
- **Missing `gh` CLI**: Silently fallback to WebFetch (this is expected and normal)
- **`gh` authentication issues**: Note in output "⚠️ gh CLI available but not authenticated, using WebFetch fallback" and proceed with WebFetch
- **Invalid branch**: Show error with list of available branches using `git branch -a`
- **Invalid issue URL**: Show error with expected format: `https://github.com/{owner}/{repo}/issues/{number}`
- **Network errors with both gh and WebFetch**: Report the error and ask user to verify connectivity or provide issue details manually
- **Issue not found (404)**: Verify issue number and repository are correct

## Important Notes

- Be thorough but concise - focus on actionable insights
- Always cite specific file:line references for code evidence
- If requirements are ambiguous, note that and make reasonable assumptions
- Consider the spirit of the issue, not just literal checklist items
- Flag potential edge cases or scenarios not covered by tests
