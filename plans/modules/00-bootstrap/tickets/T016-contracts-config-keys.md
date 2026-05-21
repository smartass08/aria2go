# T016-contracts-config-keys

## T016: Write plans/contracts/config-keys.md (all ~140 CLI/config options)

| Field | Value |
|-------|-------|
| id | T016 |
| module | 00-bootstrap |
| depends_on | [] |
| blocked_by | [] |
| status | in_review |
| complexity | L |
| claimed_by | deepseek-v4pro-t016-spec-001 |
| claimed_at | 2026-05-19T10:00:00Z |
| claim_ttl_seconds | 7200 |
| gates | go-vet-adr-check |
| target_files | plans/contracts/config-keys.md |
| context_files | source-truth/aria2-docs/_sources/manual.rst |
| context_budget_tokens | 4000 |

## Implementation Notes

SPEC AUTHOR ROLE. Each option: name, alias, type, value grammar, default, category, accumulator behavior.

## Implementation Log

Status: `in_review`. 
- Documented all options from aria2c manual with 9 categories and ~197 option entries (some options appear in multiple applicable contexts).
- Includes: precedence rules, value grammar table, accumulative options list, environment variables, config file syntax, proxy credential ordering, control file notes, event hook interface.
- Gate `go-vet-adr-check`: informational (no Go code). Spec authored from clean-room reading of manual.
