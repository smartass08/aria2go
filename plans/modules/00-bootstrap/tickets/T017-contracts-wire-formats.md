# T017: Wire Formats Contract Spec

**Module:** 00-bootstrap  
**Status:** in_review  
**Claimed by:** deepseek-v4pro-t017-spec-001  
**Claimed at:** 2026-05-19T12:00:00Z  

## Implementation Notes

Create `plans/contracts/wire-formats.md` covering:

1. Bencode encoding (BEP 3 §bencoding)
2. .torrent metainfo file format (BEP 3 §metainfo, BEP 27)
3. Magnet URI scheme (BEP 9 §magnet URI format)
4. WebSocket framing (RFC 6455)
5. aria2.session format (summary, full spec at plans/byte-compat/session-format.md)

Source material: `source-truth/beps/beps/bep_0003.rst`, `bep_0009.rst`, `bep_0010.rst` (public domain spec text).

## Gating

- [ ] `go-vet-adr-check`: Spec only — no Go code. Gate is informational pass.
- [ ] Spec reviewed for completeness against all 5 sections.
