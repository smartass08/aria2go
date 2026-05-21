---
id: T002
module: 00-bootstrap
complexity: S
priority: 1
depends_on: []
target_files: [LICENSE]
test_files: []
context_files:
  - ENTRYPOINT.md
context_budget_tokens: 800
gates: []
contract_surface:
  cli: []
  rpc: []
  session: []
  config: []
  fixtures: []
---

# T002: Add Apache-2.0 LICENSE file

## Goal
Add the canonical Apache-2.0 license text to the repo root as `LICENSE`. After this ticket, the project's license is unambiguously declared and CI license-checkers (when added) pass.

## Why This Matters
aria2go is an Apache-2.0 clean-room rewrite. The LICENSE file is the legal anchor: it declares our license to downstream users and is the file every license scanner (ours and theirs) looks for first. It is also the file that establishes the boundary against aria2's GPLv2+ source (which lives only under `source-truth/`).

## Acceptance Criteria
1. `LICENSE` exists at the project root.
2. The file contains the verbatim, full Apache-2.0 license text, exactly as published at https://www.apache.org/licenses/LICENSE-2.0.txt.
3. The file is plain ASCII text (no BOM, LF line endings).
4. After the standard preamble, the "APPENDIX: How to apply the Apache License to your work" section IS included (it is part of the canonical text; do not strip it).
5. The "Copyright" line at the bottom of the appendix template is filled in:
   ```
   Copyright 2026 The aria2go Authors

   Licensed under the Apache License, Version 2.0 (the "License");
   ...
   ```

## Contract Surface
- CLI: none
- RPC: none
- Session: none
- Config: none
- Fixtures: none

## Context (≤3 files)
- `ENTRYPOINT.md` — confirms Apache-2.0 + clean-room status.

## Implementation Notes
Source: https://www.apache.org/licenses/LICENSE-2.0.txt (or the canonical text from `source-truth/aria2/COPYING`? — NO; that's aria2's COPYING which is GPLv2+ and must not be referenced). Use the Apache Foundation canonical text only.

Many projects abbreviate by stripping the "How to apply" appendix; for aria2go we keep it because it doubles as the copyright-notice template the rest of the project uses in file headers.

The trailing copyright line is the only thing customized:

```
Copyright 2026 The aria2go Authors
```

(No individual name; the project owner is the aggregate of contributors.)

## Error Cases & Validation
None — this is a static file.

## Out of Scope
- `NOTICE` (separate ticket T003; populated under path b only).
- Per-source-file license headers (will be added by individual implementation tickets via a header template).

## References
- ADR-0014 / Master plan §1.3 confirms Apache-2.0.
- https://www.apache.org/licenses/LICENSE-2.0.txt (canonical text).

## Estimated Tokens
- Context: 500   Implementation: 200   Tests: 0   Total: 700
