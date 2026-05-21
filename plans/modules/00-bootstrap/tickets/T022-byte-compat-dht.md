# T022: DHT Routing Table File Format Spec

**Module:** 00-bootstrap  
**Status:** in_review  
**Claimed by:** deepseek-v4pro-t022-spec-001  
**Claimed at:** 2026-05-19T10:00:00Z  

## Implementation Notes

Create `plans/byte-compat/dht-file-format.md` covering:

1. File purpose: persists DHT routing table nodes between sessions (`dht.dat`)
2. Binary structure:
   - Header (magic `0xa1 0xa2`, format ID `0x02`, version `0x0003`)
   - Timestamp (u64 big-endian for v3, u32 big-endian for v2)
   - Local node record (32 bytes: 8 reserved + 20 ID + 4 reserved)
   - Node count (u32 big-endian + 4 reserved)
   - Per-node entry (56 bytes fixed: compact length + 7 reserved + compact IP/port + padding to 24 + 20 ID + 4 reserved)
3. Network byte order for all multi-byte integers
4. Compact IP:port format (4+2 for v4, 16+2 for v6)
5. Version detection (v2 header vs v3 header)
6. Atomic write: temp file + rename
7. Error handling: skip corrupt entries; reject on bad header; empty table on failure
8. Compatibility notes (v2→v3 timestamp widening, family-specific files)

Source material: `source-truth/aria2/src/DHTRoutingTableSerializer.cc/.h`, `DHTRoutingTableDeserializer.cc`

## Gating

- [x] `go-vet-adr-check`: Spec only — no Go code. Gate is informational pass.
- [ ] Spec reviewed for completeness against all 8 sections.

## Implementation Log

No divergence from Implementation Notes.
