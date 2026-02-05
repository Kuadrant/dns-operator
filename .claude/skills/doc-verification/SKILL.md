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

## Quick Reference Checklist

**Before reporting ANY issue, verify ALL of these:**

- [ ] **Have code evidence**: Specific file:line references proving the issue
- [ ] **Checked official docs**: Verified syntax/conventions against authoritative sources
- [ ] **Understood document scope**: Confirmed it's in scope for this document type
- [ ] **Read related code**: Traced execution to verify understanding
- [ ] **Assessed confidence**: Issue is "Confirmed" not "Uncertain"
- [ ] **Not a false positive**: Passed through false positive checklist
- [ ] **Verified context**: Checked if issue applies to all deployment contexts
- [ ] **Researched first**: Didn't ask user what you can research yourself

**Quality over quantity:**
- ✅ 3 confirmed issues with evidence > 30 unverified claims
- ✅ Verify BEFORE reporting, not after
- ✅ When uncertain, research more or skip
- ✅ False positives damage trust more than missing real issues

**Most common false positives to avoid:**
- Valid alternative syntax (check official docs first)
- Content intentionally out of scope (check document type)
- Standard industry conventions (not "confusing", just unfamiliar)
- Architecture working differently than assumed (trace code first)
- Basic docs linking to comprehensive docs (not "incomplete")

## Step 0: Verification Principles (CRITICAL - READ FIRST)

Before beginning documentation verification, internalize these principles learned from real-world documentation reviews:

### Principle 1: Code Evidence is MANDATORY

**Every accuracy claim MUST be backed by specific code evidence:**

- ✅ CORRECT: "Line 94 shows default is 'admin.example.com' but code at zone.go:76-85 shows default is 'hostmaster.{zone}'"
- ❌ WRONG: "The SOA RNAME default looks incorrect"

**Required for each claim:**
- Exact file path and line numbers in both documentation and code
- Quote from documentation showing the issue
- Code snippet proving the discrepancy
- Confidence level (Confirmed/Likely/Uncertain)

### Principle 2: Understand Before Claiming

**Common false positives to avoid:**

1. **Syntax/Convention Misunderstanding:**
   - Before claiming syntax is wrong, check official documentation
   - Examples:
     - CoreDNS: rewrite syntax without "stop" directive is VALID (check CoreDNS docs)
     - Kubernetes: "53 → 1053" port mapping is STANDARD notation (externalPort → containerPort)
     - DNSPolicy: spec syntax variations may be valid for different API versions
     - Health checks: probe configuration syntax may follow Kubernetes standards

2. **Scope Misunderstanding:**
   - Understand the document's purpose before claiming content is missing
   - Basic/overview docs intentionally link to comprehensive docs rather than duplicating content
   - Examples:
     - Provider overview docs (provider.md) link to comprehensive configuration guides
     - Plugin README files provide basics with links to detailed architecture docs
     - User guides focus on practical workflows, not exhaustive API reference
     - Architecture docs cover design, not operational procedures

3. **Architecture Misunderstanding:**
   - Verify your understanding of system architecture before claiming inaccuracies
   - Complex features may work differently than assumed - trace through code first
   - Examples:
     - Combined routing strategies (GEO+weighted) may use CNAME chain mechanisms
     - Multi-cluster delegation may sync via indirect controller patterns
     - Health check propagation may go through status updates, not direct DNS changes
     - Zone matching may happen at provider level, not controller level

4. **Standard Conventions:**
   - Don't flag industry-standard conventions as "confusing"
   - Examples:
     - Port mapping notation (external → internal)
     - RFC email formats (user.name@domain becomes user\.name.domain in DNS)
     - Kubernetes label patterns (kuadrant.io/feature-name)
     - CRD naming conventions (plurals, API groups)

### Principle 3: Document Purpose and Scope

**Before analyzing any document, determine:**

1. **Document Type:**
   - Overview/Basic (links to comprehensive docs)
   - Comprehensive Reference (detailed technical documentation)
   - User Guide (practical examples and workflows)
   - Architecture (system design and patterns)

2. **Target Audience:**
   - New users (getting started)
   - Developers (implementation details)
   - Operators (deployment and configuration)
   - Contributors (development workflow)

3. **Scope Boundaries:**
   - What features/topics are explicitly IN scope
   - What features/topics are explicitly OUT of scope
   - What's covered elsewhere and linked

**Adjust expectations based on document type:**
- Basic docs: Expect links to detailed docs, not exhaustive coverage
- Comprehensive docs: Expect detailed technical accuracy
- User guides: Expect working examples and practical scenarios

### Principle 4: Verification Workflow

**For EACH potential issue:**

1. **Identify** - Note potential issue
2. **Research** - Read related code, check official docs, understand context
3. **Verify** - Gather specific code evidence
4. **Assess Confidence:**
   - Confirmed: Code evidence clearly shows issue (include in report)
   - Likely: Strong indicators but some ambiguity (flag for review)
   - Uncertain: Might be issue but could be misunderstanding (DON'T report)
5. **Document** - Only report Confirmed issues with full evidence

### Principle 5: Aggressive = Thorough Verification, Not More Claims

**"Aggressive review" means:**
- ✅ Thoroughly verify each claim with code evidence
- ✅ Check edge cases and different deployment contexts
- ✅ Verify examples actually work as documented
- ✅ Cross-reference related documentation for consistency

**"Aggressive review" does NOT mean:**
- ❌ Reporting more issues without verification
- ❌ Flagging things that "look wrong" without proof
- ❌ Claiming missing content without checking document scope
- ❌ Reporting standard conventions as problems

### Principle 6: Confidence Levels and Reporting

**Only report issues with high confidence:**

- **Report:** Factual inaccuracies with code proof
- **Report:** Misleading wording that causes confusion
- **Report:** Broken references, wrong paths, incorrect defaults
- **Report:** Missing context (e.g., path differs between deployment environments)
- **Don't Report:** "Could be clearer" without specific confusion case
- **Don't Report:** "Might be outdated" without evidence
- **Don't Report:** Personal preferences on organization/style

### Principle 7: Context-Aware Documentation

**Document for all relevant contexts:**

Different deployment/usage contexts may require different information:

1. **Environment Variations:**
   - Container deployment vs local development (different paths, configs)
   - Production vs testing (different databases, credentials)
   - Single-cluster vs multi-cluster (different setup steps)
   - Cloud provider variations (AWS vs GCP vs Azure vs self-hosted)

2. **Detecting Context Issues:**
   - Look for absolute paths that might differ between environments
   - Check for hardcoded values that vary by context
   - Verify examples work in all documented deployment scenarios
   - Flag where documentation assumes only one context

3. **Examples:**
   - GeoIP database: `/GeoLite2-City.mmdb` (container) vs `geoip/GeoLite2-City.mmdb` (local)
   - Port bindings: Different in Kind vs production Kubernetes
   - Service endpoints: localhost vs cluster IPs vs external IPs
   - File paths: Container paths vs host mount paths

### Principle 8: Iterative Understanding

**When uncertain, re-read and research before claiming:**

1. **If documentation seems wrong:**
   - Re-read the section carefully
   - Check related sections for context
   - Read the code it references
   - Search official documentation
   - Only then assess if it's actually wrong

2. **If you don't understand the architecture:**
   - Don't claim the documentation is wrong
   - Trace through the code to understand
   - Read related documentation files
   - Look for design docs or ADRs
   - If still uncertain, skip or ask for clarification

3. **Signs you need to re-read:**
   - Your claim contradicts official docs
   - The code seems to contradict your understanding
   - You're uncertain about your interpretation
   - The documentation seems self-contradictory

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

**CRITICAL: Every accuracy issue requires code verification**

Compare documentation against actual code using this rigorous process:

1. **Commands verification:**
   ```bash
   # For each documented command, verify it exists
   # Example: Document says "make test-unit"
   grep -r "test-unit" Makefile  # Verify target exists
   make test-unit --dry-run      # Verify it runs (if safe)
   ```

2. **Configuration verification:**
   ```bash
   # For each documented flag/env var, find in code
   # Example: Document says "--delegation-role=primary"
   grep -r "delegation-role" cmd/ internal/  # Find flag definition
   # Verify default value, allowed values, description match docs
   ```

3. **Default values verification:**
   ```bash
   # For each documented default, verify in code
   # Example: Document says "default: 5s"
   grep -A5 -B5 "MinRequeueTime" cmd/  # Find actual default
   ```

4. **Code references verification:**
   - Check if documented file paths exist: `ls -la {path}`
   - Verify function/type names: `grep -r "type {Name}" .`
   - Validate syntax in examples: check against official docs

5. **Path/Location verification:**
   ```bash
   # Example: GeoIP database path differences
   # Container path: Check Dockerfile COPY commands
   grep -r "GeoLite2-City-demo.mmdb" Dockerfile
   # Runtime path: Check configuration files
   grep -r "GeoLite2" config/
   # Document both if they differ!
   ```

**Verification Evidence Template:**

For each accuracy issue found:
```
CLAIM: {What documentation says that's wrong}
DOC LOCATION: {file}:{line}
DOC QUOTE: "{exact quote from documentation}"

CODE EVIDENCE:
- File: {file}:{line}
- Code: `{code snippet showing actual behavior}`
- Proof: {explanation of discrepancy}

CONFIDENCE: Confirmed | Likely | Uncertain
(Only report if Confirmed with clear evidence)
```

### C. Cross-File Consistency and Link Validation

**Verify consistency across documentation files:**

1. **Link Validation:**
   ```bash
   # Find all markdown links
   grep -rn "\[.*\](.*)" docs/ *.md

   # For each internal link, verify target exists
   # Example: [CoreDNS Config](docs/coredns/configuration.md)
   # Check: ls docs/coredns/configuration.md

   # Common issues:
   # - File was renamed/moved but links not updated
   # - File was deleted but links remain
   # - Incorrect relative paths
   ```

2. **Cross-Reference Consistency:**
   ```bash
   # Find all file/path references in documentation
   grep -rn "`[a-zA-Z/_.-]*\.go`\|`[a-zA-Z/_.-]*\.md`" docs/ *.md

   # Verify each referenced file exists
   # Check line numbers are approximately correct
   ```

3. **Terminology Consistency:**
   - Check that technical terms are used consistently
   - Example: "DNSRecord" vs "DNS Record" vs "dns record"
   - Example: "multi-cluster" vs "multicluster"
   - Create a terminology list if needed

4. **Example Consistency:**
   - Verify examples across files use same patterns
   - Check that example resource names are consistent
   - Ensure example values are realistic and correct

5. **Version/Name Consistency:**
   ```bash
   # Check for version references
   grep -rn "version:\|v[0-9]" docs/ *.md

   # Check for API group references
   grep -rn "apiVersion:" docs/ *.md

   # Verify they match current API versions
   ```

6. **Command Name Consistency:**
   ```bash
   # Find all command references
   grep -rn "kubectl.*\|make.*\|.*_dns" docs/ *.md

   # Common issues to check:
   # - Old command name (kubectl-dns vs kubectl-kuadrant_dns)
   # - Inconsistent formatting (backticks vs code blocks)
   # - Wrong binary name
   ```

### D. Freshness Check

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

### E. Quality Assessment

Evaluate documentation quality:

- **Clarity**: Is language clear and unambiguous?
- **Organization**: Logical structure and easy navigation?
- **Examples**: Sufficient, working examples provided?
- **Completeness**: All major features covered?
- **Consistency**: Terminology and formatting consistent?
- **Accessibility**: Appropriate for target audience?

### F. False Positive Prevention Checklist

Before reporting an issue, verify it's not a false positive:

**Syntax/Format Issues:**
- [ ] Checked official documentation for correct syntax
- [ ] Verified against language/framework specifications
- [ ] Confirmed it's not a valid alternative syntax
- [ ] Tested example (if possible) to confirm it fails

**Missing Content Issues:**
- [ ] Confirmed it's in scope for this document type
- [ ] Checked if content exists in linked comprehensive docs
- [ ] Verified target audience needs this information here
- [ ] Confirmed it's not intentionally out of scope

**Architecture/Design Issues:**
- [ ] Fully understood the system design from code
- [ ] Traced execution flow to verify documentation claim
- [ ] Checked for indirect mechanisms (CNAME chains, etc.)
- [ ] Confirmed my understanding is correct

**Convention/Style Issues:**
- [ ] Verified it's not an industry-standard convention
- [ ] Checked if it follows project's established patterns
- [ ] Confirmed it actually causes confusion (not just unfamiliar)
- [ ] Assessed if change would materially improve clarity

**Evidence Quality:**
- [ ] Have specific file:line references for both doc and code
- [ ] Can provide exact quotes showing discrepancy
- [ ] Confidence level is "Confirmed" not "Uncertain"
- [ ] Issue is factual (not opinion/preference)

## Step 4: Systematic Missing Documentation Detection

**Goal:** Proactively discover undocumented code elements rather than relying on manual review.

### A. Discover Code Elements That Should Be Documented

Use systematic code analysis to find what exists in the codebase:

**1. Make Targets (Build Commands):**

**IMPORTANT:** Make targets may be spread across multiple files (.mk files) in various directories. Always follow the include chain from the root Makefile to discover all targets.

```bash
# Step 1: Find all makefiles in the project
# Start with the root Makefile and discover all included .mk files
find . -name "Makefile" -o -name "*.mk" | grep -v vendor | grep -v node_modules | sort

# Step 2: Extract include directives from root Makefile to understand the chain
grep -E "^include |^-include " Makefile | sed 's/^-\?include //'

# Step 3: Common locations for .mk files (check all of these)
# - make/ directory (e.g., make/targets.mk, make/tests.mk)
# - Root directory (e.g., common.mk, build.mk)
# - Subdirectories (e.g., hack/make/, scripts/make/)

# Step 4: Extract ALL targets from ALL makefiles
# Method 1: Comprehensive search in all makefiles
for makefile in Makefile $(find make/ -name "*.mk" 2>/dev/null) $(find . -maxdepth 1 -name "*.mk" 2>/dev/null); do
  if [ -f "$makefile" ]; then
    echo "=== Targets from $makefile ==="
    grep "^[a-zA-Z0-9_-]*:" "$makefile" | cut -d: -f1 | sort
  fi
done

# Method 2: Recursive include following (more accurate)
# Start from Makefile and follow all includes
grep -h "^[a-zA-Z0-9_-]*:" Makefile make/*.mk *.mk 2>/dev/null | cut -d: -f1 | sort -u

# Step 5: Get target descriptions (if they have comments)
# Many makefiles have ## comments describing targets
grep -h "^[a-zA-Z0-9_-]*:.*##" Makefile make/*.mk *.mk 2>/dev/null

# Step 6: Cross-reference against documentation
# For each discovered target, check if it's documented
TARGETS=$(grep -h "^[a-zA-Z0-9_-]*:" Makefile make/*.mk *.mk 2>/dev/null | cut -d: -f1 | sort -u)
for target in $TARGETS; do
  # Search in CLAUDE.md and README.md
  if ! grep -q "$target" CLAUDE.md README.md 2>/dev/null; then
    echo "Undocumented target: $target"
  fi
done
```

**Example make file structure to check:**
```
project-root/
├── Makefile              # Main makefile (check include directives here)
├── common.mk             # Common targets
├── build.mk              # Build-specific targets
├── make/
│   ├── targets.mk        # Additional targets
│   ├── tests.mk          # Test targets
│   ├── docker.mk         # Docker targets
│   └── deploy.mk         # Deployment targets
└── hack/
    └── make/
        └── ...           # More .mk files
```

**Common patterns to look for:**
- `include make/*.mk` (includes all .mk files in make/ directory)
- `include common.mk build.mk` (includes specific files)
- `-include optional.mk` (includes if exists, - prefix means don't fail if missing)
- `include $(wildcard *.mk)` (includes all .mk files in current dir)

**Identifying User-Facing vs Internal Targets:**

Not all targets need documentation. Use these heuristics to prioritize:

```bash
# User-facing targets (should be documented):
# - Common development tasks: build, test, run, deploy, clean
# - Testing: test, test-unit, test-integration, test-e2e
# - Code quality: lint, fmt, vet
# - Installation: install, uninstall
# - Helpers: help, list-targets

# Internal/helper targets (optional to document):
# - Prefixed with underscore or dot: _internal-target, .PHONY
# - Tool installation: kustomize, controller-gen, envtest
# - CI-specific: act-*, github-*
# - Build artifacts cleanup
# - Targets called by other targets but rarely used directly
```

**Quick heuristic check:**
```bash
# Count total targets
TOTAL=$(grep -h "^[a-zA-Z0-9_-]*:" Makefile make/*.mk 2>/dev/null | cut -d: -f1 | sort -u | wc -l)
echo "Total targets found: $TOTAL"

# Expected documentation coverage:
# - Small projects (<30 targets): Document 50-70%
# - Medium projects (30-100 targets): Document 30-50%
# - Large projects (>100 targets): Document 20-40%

# Focus on documenting:
# 1. All targets mentioned in README.md "Quick Start"
# 2. All targets a new developer needs for local development
# 3. All targets for testing
# 4. All targets for deployment
```

**Real-world example:**
```bash
# In dns-operator project:
# - Main Makefile has: 90 targets
# - make/*.mk files have: 72 targets
# - Total unique targets: 137 targets
# - Only checking Makefile would MISS ~50 targets (37% of total)!

# This includes important targets like:
# - coredns-* targets (in make/coredns.mk)
# - multicluster-* targets (in make/multicluster.mk)
# - CLI build targets (in make/cli.mk)
# - Act CI targets (in make/act.mk)
```

**Verification checklist:**
- [ ] Checked root Makefile for all include directives
- [ ] Searched make/ directory for .mk files
- [ ] Searched root directory for .mk files
- [ ] Checked subdirectories (hack/make/, scripts/make/, etc.)
- [ ] Extracted targets from ALL discovered makefiles (not just Makefile)
- [ ] Counted total targets vs documented targets
- [ ] Identified which targets are user-facing vs internal/helper targets
- [ ] Verified user-facing targets are documented
- [ ] Cross-referenced against CLAUDE.md and README.md
- [ ] Checked if documented targets have correct descriptions and examples

**2. CLI Flags and Environment Variables:**
```bash
# Find flag definitions (Go example)
grep -rn "flag\.\(String\|Bool\|Int\|Duration\)" cmd/ internal/ | grep -v test

# Find environment variable usage
grep -rn "os.Getenv\|LookupEnv" cmd/ internal/ | grep -v test

# Cross-reference against documented flags/env vars
```

**3. API Types / CRDs (Kubernetes operators):**
```bash
# Find all type definitions in API
grep -rn "^type.*struct" api/ | cut -d: -f1,3

# Find all CRD kinds
grep -rn "kind:" config/crd/bases/ | cut -d: -f3 | sort -u

# Check if each type has documentation
```

**4. Public Functions/Methods:**
```bash
# Find exported functions (Go example - starts with capital letter)
grep -rn "^func [A-Z]" internal/ pkg/ | grep -v "_test.go"

# Find exported types
grep -rn "^type [A-Z].*struct" internal/ pkg/ | grep -v "_test.go"
```

**5. Configuration Files:**
```bash
# Find config files
find config/ -type f -name "*.yaml" -o -name "*.json" -o -name "Corefile"

# Check if each config file is explained in documentation
```

**6. Recent Code Additions:**
```bash
# Find files added/modified in last 6 months
git log --since="6 months ago" --name-only --pretty=format: | sort -u | grep -v test

# Find new features from commit messages
git log --since="6 months ago" --oneline --grep="feat:\|feature:" | head -20

# Cross-reference against documentation updates
```

### B. Cross-Reference Against Documentation

For each discovered code element:

1. **Search documentation for mentions:**
```bash
# Example: Check if flag "--delegation-role" is documented
grep -r "delegation-role" docs/ *.md

# If found: verify accuracy
# If not found: determine if it should be documented (user-facing vs internal)
```

2. **Assess documentation priority:**
- **Critical (must document):** User-facing features, main commands, CRDs, configuration
- **High (should document):** Common workflows, important flags, major APIs
- **Medium (consider documenting):** Advanced features, optional configuration
- **Low (optional):** Internal functions, development-only tools

3. **Check documentation completeness:**
- Is it mentioned? (presence)
- Is it explained? (description)
- Are there examples? (usage)
- Are defaults documented? (configuration)

### C. Generate Missing Documentation Report

For each undocumented element, provide:

```
**Missing: {Element Type} - {Element Name}**
Priority: Critical | High | Medium | Low

Location in Code:
- File: {file}:{line}
- Definition: `{code snippet}`

Why It Matters:
- {User impact or importance}

Should Be Documented In:
- {Target documentation file}
- {Suggested section}

Suggested Content:
- {Brief description of what should be documented}
```

### D. Language-Specific Detection Commands

**For Go Projects:**
```bash
# Find all flags
grep -rn "flag\." cmd/ | grep -v test | cut -d: -f1-3

# Find all env vars
grep -rn "os.Getenv\|LookupEnv\|env:" cmd/ internal/ config/ | grep -v test

# Find all CRD markers
grep -rn "+kubebuilder" api/
```

**For Python Projects:**
```bash
# Find all CLI arguments
grep -rn "argparse\|click.command\|@argument"

# Find all environment variables
grep -rn "os.environ\|os.getenv"

# Find all public classes
grep -rn "^class [A-Z]"
```

**For JavaScript/TypeScript Projects:**
```bash
# Find all exported functions
grep -rn "^export function\|^export const"

# Find all environment variables
grep -rn "process.env"

# Find all API endpoints
grep -rn "app.get\|app.post\|router"
```

## Step 5: Code-to-Documentation Coverage Analysis

After discovering what exists (Step 4), verify documentation coverage quality:

1. **Entry points:**
   - All main commands have usage examples
   - CLI flags show default values and allowed values
   - Startup/configuration documented

2. **Public APIs:**
   - All exported functions/types have descriptions
   - API endpoints show request/response formats
   - Error cases documented

3. **Configuration:**
   - All config options listed with descriptions
   - Environment variables documented
   - Default values specified
   - Valid ranges or allowed values shown

4. **CRDs/API Types (for Kubernetes operators):**
   - All CRD specs documented
   - Field descriptions complete and accurate
   - Examples provided for common use cases
   - Status fields explained

5. **Examples Coverage:**
   - Basic use case has example
   - Advanced features have examples
   - Multi-step workflows documented
   - Edge cases addressed

## Step 6: Generate Comprehensive Report

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

## Step 7: Verify Examples

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
- **Uncertain about correctness**: DON'T report - research more or skip
- **Can't find code evidence**: DON'T report as accuracy issue - may be valid
- **Syntax looks wrong**: Check official docs before claiming error
- **Content seems missing**: Verify document scope before claiming gap
- **Architecture seems wrong**: Ensure you understand system before claiming issue
- **Standard conventions**: Don't report as "confusing" without user evidence

**When in doubt, err on side of NOT reporting rather than reporting false positives**

## Research vs Asking for Clarification

**Prioritize independent research over asking user questions:**

### When to Research Yourself (Most Cases)

Research independently when:
- **Syntax/format questions**: Check official documentation (CoreDNS docs, Kubernetes docs, RFC specs)
- **Code behavior questions**: Read the actual code, trace execution flow
- **Architectural questions**: Read related files, follow imports, understand the design
- **Example validity**: Try to understand why example is written that way
- **Missing content claims**: Verify document scope, check if content exists elsewhere

**Research steps before claiming an issue:**
1. Read the documentation section thoroughly
2. Find and read the related code
3. Search for official documentation/specifications
4. Check related documentation files
5. Verify your understanding is correct
6. Only then assess if documentation has an issue

### When to Ask User (Rare Cases)

Only ask user when:
- **Scope clarification needed**: "Should this document cover feature X, or is that intentionally out of scope?"
- **Priority questions**: "Are these 10 issues all important, or should I focus on specific types?"
- **Ambiguous requirements**: "The code shows A, but there could be valid reasons - should this be documented?"
- **Direction needed**: "I found issues in both completeness and accuracy - which should I prioritize?"

### Anti-Patterns to Avoid

**DON'T ask user to verify your claims:**
- ❌ "Is the CoreDNS rewrite syntax correct?"
- ✅ Check CoreDNS official documentation yourself

**DON'T ask user about standard conventions:**
- ❌ "Is port mapping notation 53 → 1053 standard?"
- ✅ Research Kubernetes port mapping conventions

**DON'T ask user to explain code:**
- ❌ "How does the weighted routing work?"
- ✅ Read the code in weighted.go and understand it

**DON'T ask user about document scope:**
- ❌ "Should provider.md include detailed examples?"
- ✅ Read the document, see it links to comprehensive docs, understand it's an overview

### Research Resources

Common resources to check:
- **Official project documentation**: CoreDNS docs, Kubernetes docs, cloud provider docs
- **RFCs and specifications**: DNS RFCs, HTTP specs, etc.
- **Code comments and godoc**: Often explain design decisions
- **Related documentation files**: Check if info exists elsewhere
- **Git history**: Understand why something was changed
- **Issue tracker**: May explain design decisions or known limitations

**Critical Additions (from Real-World Experience):**

- **Verification is non-negotiable**: Every accuracy claim requires specific code evidence with file:line references
- **Understand document scope**: Basic docs with links are not "incomplete" - that's their intended design
- **Check official documentation first**: Before claiming syntax errors, verify against authoritative sources
- **Context matters**: Path differences, deployment variations - document all contexts
- **False positives damage trust**: One careful, verified issue is better than ten unverified claims
- **Scope boundaries must be respected**: If reviewing specific features, don't flag out-of-scope topics as "missing"
- **Standard conventions are not issues**: Port mappings, RFC formats, Kubernetes patterns are not problems
- **Distinguish misleading from unfamiliar**: Something being new to you doesn't make it wrong

**Original Guidelines:**

- Be thorough but constructive - focus on actionable improvements
- Always provide specific file:line references for issues found
- Distinguish between "definitely wrong" and "possibly outdated"
- Consider the project's maturity (early stage vs established)
- Prioritize user-facing documentation accuracy over internal notes
- Flag breaking changes or deprecations not mentioned in docs
- Suggest documentation structure improvements where applicable
- Note if documentation follows industry best practices (OpenAPI, JSDoc, etc.)

## Workflow: Verify Then Report (Not Report Then Verify)

**Critical lesson from experience: Quality over quantity**

### The Right Approach

**Workflow:**
1. **Identify** potential issues during review
2. **Verify** each issue with code evidence BEFORE reporting
3. **Report** only confirmed issues with full evidence
4. **Summary** at end showing what was verified

**Example of good workflow:**
- Review document, note 15 potential issues
- Verify each against code/official docs
- Find 3 are confirmed, 12 are false positives
- Report only the 3 confirmed issues with evidence
- Mention: "Reviewed 15 potential issues, verified 3 as confirmed problems"

### The Wrong Approach (Anti-Pattern)

**DON'T do this:**
1. ❌ Identify 70+ potential issues
2. ❌ Report all issues without verification
3. ❌ User has to ask "verify each claim"
4. ❌ Then discover most are false positives
5. ❌ Wastes user time and damages trust

**Why this is problematic:**
- Creates verification burden for user
- False positives obscure real issues
- Damages credibility of the review
- Makes it hard to prioritize fixes
- User can't trust the report

### Progressive Verification for Large Reviews

**If reviewing many documents:**

1. **Start with high-priority documents:**
   - User-facing guides
   - Main README/CLAUDE.md
   - Getting started documentation

2. **Verify as you go:**
   - Don't accumulate unverified claims
   - Verify each issue immediately when found
   - Discard false positives immediately

3. **Report in batches (optional):**
   - Complete verification of 2-3 files
   - Report confirmed issues from those files
   - Move to next batch
   - OR: Wait until all done and report everything at once (with all issues already verified)

4. **Quality metrics to track:**
   - Potential issues identified: X
   - Verified as confirmed: Y
   - False positives discarded: Z
   - Verification rate: Y/X (aim for >80% - means good initial filtering)

### Red Flags in Your Own Process

**Warning signs you're not verifying enough:**
- You're using "might be", "possibly", "seems like" frequently
- You're reporting more than 5-10 issues per document
- You haven't checked official documentation
- You haven't read the related code
- You're uncertain about your claims
- You're planning to "verify later"

**If you notice these, STOP and verify before continuing.**

## Recognizing Good Documentation (Balance Critical Review)

**Not everything that's different is wrong - recognize what's working well:**

### Signs of Good Documentation

1. **Intentional Design Choices (Not Flaws):**
   - Basic/overview docs that link to detailed docs (efficient, not incomplete)
   - Examples using simplified scenarios (clear, not unrealistic)
   - Focusing on common use cases (practical, not limited)
   - Progressive disclosure (starting simple, then advanced)

2. **Valid Alternative Approaches:**
   - Different organization than you'd choose (preference, not wrong)
   - Different level of detail (appropriate for audience)
   - Different examples than you'd pick (still valid)
   - Different terminology (consistent with project conventions)

3. **Context-Appropriate Content:**
   - User guides focus on workflows, not API details (correct)
   - Architecture docs explain design, not procedures (correct)
   - README gives overview, not exhaustive reference (correct)
   - Plugin docs show basics, link to comprehensive guides (correct)

### Red Flags in Your Own Critique

**Warning signs you're being too critical:**
- Most of your issues are "could be improved" rather than "factually wrong"
- You're suggesting reorganization based on personal preference
- You're flagging "missing" content that's in linked documents
- You're reporting more than 10 issues per document
- You're using phrases like "might want to consider", "could also include"
- You're critiquing examples that are actually correct, just different

**When you notice these, recalibrate:**
- Focus on factual errors, not improvements
- Separate "wrong" from "different style"
- Trust the document author's judgment on organization
- Recognize documentation is often evolving and "good enough" beats "perfect"

### Positive Patterns to Highlight

When reviewing, actively look for and acknowledge:
- Clear, working examples
- Good cross-references between docs
- Accurate code references
- Helpful context and explanations
- Good organization and structure
- Recent updates reflecting current code
- Appropriate scope for document type

**Include these in your report's "Strengths" section.**

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