---
name: doc-verification
description: Comprehensive verification of project documentation for completeness, accuracy, consistency, and quality. Use this skill when you need to verify documentation alignment with code, check for missing or outdated content, assess documentation quality, or ensure all major code aspects are properly documented. Explicitly triggered by user commands like /doc-verification or when asked to "check documentation", "verify docs", or "review documentation".
---

# Documentation Verification

Verify project documentation for completeness, accuracy, and alignment with the current codebase. This helps ensure documentation stays up-to-date as code evolves and provides comprehensive guidance for developers.

## Overview

This skill performs comprehensive documentation analysis covering:
- **Completeness**: Missing sections, TODOs, coverage of all major code aspects
- **Accuracy**: Alignment between documentation and actual implementation
- **Consistency**: Cross-references, terminology, broken links
- **Quality**: Clarity, examples, formatting

## Step 1: Identify Documentation Files

Discover all documentation in the repository:

```bash
# Find all markdown files
find . -type f -name "*.md" -not -path "*/node_modules/*" -not -path "*/vendor/*" | sort

# Find documentation directories
find . -type d \( -name "docs" -o -name "documentation" -o -name "doc" \) -not -path "*/node_modules/*" -not -path "*/vendor/*"
```

Key documentation to analyze:
- **CLAUDE.md**: Developer guidance for AI assistants
- **README.md**: Project overview and getting started
- **docs/**: Extended documentation
- **API documentation**: Comments, generated docs, OpenAPI specs
- **Code examples**: In documentation or examples/
- **CONTRIBUTING.md**: Contribution guidelines (if exists)
- **Architecture docs**: Design documents, ADRs

## Step 2: Analyze Project Structure

Understand the current codebase to compare against documentation:

1. **Project overview:**
```bash
# Get repository structure
tree -L 3 -d -I 'node_modules|vendor|.git' || find . -type d -not -path "*/node_modules/*" -not -path "*/vendor/*" -not -path "*/.git/*" | head -50
```

2. **Key components:**
- Identify main packages/modules
- List controllers, services, APIs
- Find configuration files and flags
- Locate test suites

3. **Recent changes:**
```bash
# Check recent commits that might affect documentation
git log --oneline --since="3 months ago" --grep="breaking\|feature\|deprecate" | head -20

# Check recently modified code files
git log --since="3 months ago" --name-only --pretty=format: | grep -E '\.(go|py|js|ts|java)$' | sort -u | head -30
```

## Step 3: Comprehensive Documentation Review

For each documentation file, perform these checks:

### A. Completeness Analysis

**For CLAUDE.md (if exists):**
- [ ] Project overview and purpose clearly stated
- [ ] Common commands documented with examples
- [ ] Development setup instructions
- [ ] Testing procedures (unit, integration, e2e)
- [ ] Build and deployment processes
- [ ] Architecture overview
- [ ] Core components documented
- [ ] API types and their purposes
- [ ] Configuration flags/environment variables
- [ ] Code layout and directory structure
- [ ] Logging conventions
- [ ] Important development notes
- [ ] Links to external documentation

**For README.md (if exists):**
- [ ] Project title and description
- [ ] Quick start guide
- [ ] Installation instructions
- [ ] Basic usage examples
- [ ] Prerequisites clearly stated
- [ ] Links to detailed documentation
- [ ] License information
- [ ] Contributing guidelines or link
- [ ] Status badges (build, coverage, etc.)
- [ ] Contact/support information

**For docs/ (if exists):**
- [ ] Architecture documentation
- [ ] API reference
- [ ] User guides
- [ ] Operator/admin guides
- [ ] Troubleshooting guides
- [ ] FAQ
- [ ] Examples and tutorials

### B. Accuracy Verification

Compare documentation against actual code:

1. **Commands verification:**
   - For each documented command/make target, verify it exists
   - Check if command flags/arguments are correct
   - Validate examples actually work

2. **Configuration verification:**
   - Cross-reference documented flags with actual code
   - Check environment variable names
   - Verify default values match implementation

3. **Code references verification:**
   - Check if documented file paths exist
   - Verify function/type names are correct
   - Ensure line numbers (if any) are approximately correct

4. **Feature verification:**
   - Confirm documented features exist in code
   - Check if documented APIs match implementation
   - Validate architectural descriptions against code structure

### C. Freshness Check

Identify potentially outdated content:

1. **Stale references:**
   - Deprecated functions or types mentioned
   - Old API versions
   - Removed commands or flags
   - Changed package names

2. **Missing recent additions:**
   - New features from last 3-6 months
   - New configuration options
   - New APIs or endpoints
   - Breaking changes not documented

3. **Version mismatches:**
   - Dependency versions mentioned vs actual
   - API versions in examples
   - Framework/runtime versions

### D. Quality Assessment

Evaluate documentation quality:

- **Clarity**: Is language clear and unambiguous?
- **Organization**: Logical structure and easy navigation?
- **Examples**: Sufficient, working examples provided?
- **Completeness**: All major features covered?
- **Consistency**: Terminology and formatting consistent?
- **Accessibility**: Appropriate for target audience?

## Step 4: Code-to-Documentation Coverage

For critical code elements, check documentation coverage:

1. **Entry points:**
   - Main functions/commands documented
   - CLI flags and arguments explained

2. **Public APIs:**
   - All exported functions/types documented
   - API endpoints documented
   - Request/response formats shown

3. **Configuration:**
   - All config options documented
   - Environment variables listed
   - Default values specified

4. **CRDs/API Types (for Kubernetes operators):**
   - All CRD specs documented
   - Field descriptions complete
   - Examples provided

## Step 5: Generate Comprehensive Report

Provide a structured analysis:

### 📊 Documentation Overview

```
**Scope Analyzed:** {all | specific files}
**Documentation Files Found:** {count}
**Last Documentation Update:** {date from git log}
**Last Code Update:** {date from git log}
**Documentation Age:** {gap between doc and code updates}

**Quick Stats:**
- ✅ {X}% documentation completeness
- ⚠️ {N} potential accuracy issues
- 📅 {N} potentially outdated sections
- 🔍 {N} missing documentation items
- 💡 {N} improvement suggestions
```

### 📋 Files Analyzed

List all documentation files with:
- File path (as clickable reference)
- Last modified date
- Line count
- Primary purpose

### ✅ Strengths

Highlight what's well-documented:
- Well-covered areas
- Clear, helpful sections
- Good examples
- Recent updates

### ⚠️ Accuracy Issues

List potential inaccuracies found:

```
**Issue: {description}**
Location: {file}:{line} or {file}:{section}
Severity: Critical | High | Medium | Low

Problem:
- {what's wrong}

Current Documentation Says:
> {quote from docs}

Actual Implementation:
- {what code shows}
- {file references}

Suggested Fix:
{proposed correction}
```

### 📅 Potentially Outdated Content

Flag sections that may need updates:

```
**Section: {section name}**
File: {file}:{line}
Last Modified: {date}
Risk: High | Medium | Low

Indicators:
- {why it might be outdated}
- {recent code changes that might affect it}

Recommendation:
{what to review/update}
```

### 🔍 Missing Documentation

Identify gaps in documentation:

```
**Category: {Commands | Features | APIs | Configuration | etc}**
Priority: Critical | High | Medium | Low

Missing Items:
- {specific undocumented item}
  - Found in: {file}:{line}
  - Should be documented in: {target doc file}
  - Why it matters: {importance}
```

### 💡 Improvement Recommendations

Provide actionable suggestions organized by priority:

**Critical Priority:**
- {suggestion} - impacts core functionality/onboarding
  - Action: {specific task}
  - Files: {where to update}

**High Priority:**
- {suggestion} - important for developers/users
  - Action: {specific task}
  - Files: {where to update}

**Medium Priority:**
- {suggestion} - improves clarity/completeness
  - Action: {specific task}
  - Files: {where to update}

**Low Priority:**
- {suggestion} - nice to have enhancements
  - Action: {specific task}
  - Files: {where to update}

### 🎯 Overall Assessment

```
**Status:** ✅ Excellent | ✨ Good | ⚠️ Needs Updates | ❌ Significant Gaps

**Completeness:** {percentage}%
**Accuracy:** {percentage}%
**Freshness:** {percentage}%

**Summary:**
{2-3 paragraph assessment of overall documentation quality}

**Top 3 Action Items:**
1. {most important thing to address}
2. {second most important}
3. {third most important}
```

### 📝 Detailed Findings by File

For each documentation file, provide:

```
#### {filename}

**Overall:** ✅ Good | ⚠️ Needs Work | ❌ Outdated
**Completeness:** {percentage}%
**Accuracy:** {percentage}%

**Strengths:**
- {what's good}

**Issues:**
- {problems found with line references}

**Suggestions:**
- {improvements}
```

## Step 6: Verify Examples

For code examples in documentation:

1. **Syntax check:**
   - Examples use correct syntax
   - Examples follow current API

2. **Runnable verification:**
   - If possible, test that examples work
   - Check if example files exist and are current

3. **Consistency check:**
   - Examples follow consistent style
   - Examples use current best practices

## Error Handling

Handle these scenarios gracefully:

- **No documentation found**: Recommend creating essential docs (README, CLAUDE.md)
- **Documentation parsing errors**: Note the issue and continue with other files
- **Git history unavailable**: Skip freshness checks, focus on accuracy
- **Large documentation set**: Summarize and focus on most critical files
- **Ambiguous code**: Note uncertainty in accuracy assessment

## Important Notes

- Be thorough but constructive - focus on actionable improvements
- Always provide specific file:line references for issues found
- Distinguish between "definitely wrong" and "possibly outdated"
- Consider the project's maturity (early stage vs established)
- Prioritize user-facing documentation accuracy over internal notes
- Flag breaking changes or deprecations not mentioned in docs
- Suggest documentation structure improvements where applicable
- Note if documentation follows industry best practices (OpenAPI, JSDoc, etc.)

## Helpful Analysis Tips

1. **Use grep/glob strategically:**
   - Search for documented command names in actual code
   - Find flag definitions to verify documented options
   - Locate type definitions mentioned in docs

2. **Check consistency:**
   - Compare terminology between different doc files
   - Verify examples use consistent patterns
   - Check formatting consistency

3. **Consider audience:**
   - Is CLAUDE.md appropriate for AI assistants?
   - Is README appropriate for new users?
   - Are advanced docs appropriate for experienced developers?

4. **Look for common documentation debt:**
   - TODOs in documentation
   - "Coming soon" sections never completed
   - Placeholder content
   - Broken internal links