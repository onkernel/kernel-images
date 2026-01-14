# Migrate TTY Attach from HTTP Hijack to WebSocket

**Status: IMPLEMENTED**

## Background

The current implementation uses HTTP connection hijacking for TTY attach (`/process/{process_id}/attach`). While this works for direct connections, it has issues when multiple proxy layers are involved (e.g., load balancers, reverse proxies, API gateways). WebSockets provide a more robust solution for bidirectional streaming that is well-understood by intermediary infrastructure.

### Current Implementation (kernel-images)

```
Client ──HTTP GET──> /process/{id}/attach ──HTTP Hijack──> Raw TCP Bidirectional I/O
                     │
                     └── server/cmd/api/api/process.go:HandleProcessAttach()
                         - Validates process exists and is PTY-backed
                         - Enforces single concurrent attach per process
                         - Hijacks HTTP connection
                         - Bidirectional copy between PTY and raw TCP socket
                         - Uses unix.Poll() for non-blocking PTY reads
```

**Problems with HTTP Hijack:**
1. Many proxies/load balancers don't properly support HTTP connection hijacking
2. Connection upgrades are non-standard and poorly understood by infrastructure
3. No message framing - raw bytes, hard to add metadata (resize events, exit codes)
4. Debugging/monitoring is difficult - opaque binary stream
5. No reconnection support if connection drops

### Reference Implementation (hypeman)

Hypeman uses a well-designed WebSocket-based architecture for exec into VMs:

```
Client ──WebSocket──> /instances/{id}/exec ──WebSocket Protocol──> API Server
                      │                                             │
                      └─ JSON request in first message              │
                      └─ Binary stdin/stdout thereafter             │
                      └─ JSON exit code at end                      │
                                                                    │
                                              ┌─────────────────────┘
                                              │
                                              v
                                        gRPC over vsock
                                              │
                                              v
                                        Guest Agent (in VM)
                                              │
                                              v
                                        PTY / Command Execution
```

**Key Design Patterns from hypeman:**

1. **WebSocket Upgrade**: Uses `gorilla/websocket` with standard upgrade
2. **Protocol**: 
   - First message: JSON with `{command, tty, env, cwd, timeout}`
   - Subsequent messages: Binary for stdin/stdout
   - Final message: JSON with `{exitCode: N}`
3. **Bidirectional Streaming**: `io.ReadWriter` wrapper around WebSocket
4. **Message Types**: TextMessage for JSON, BinaryMessage for data

## Proposed Design

### New Endpoint: `GET /process/{process_id}/attach` (WebSocket)

Replace the HTTP hijack endpoint with a WebSocket endpoint that follows hypeman's pattern but adapted for our simpler use case (no VM/vsock, just local PTY).

### Protocol

```
1. Client opens WebSocket to /process/{process_id}/attach
2. Server validates process exists, is PTY-backed, and not already attached
3. Bidirectional streaming:
   - Client → Server: Binary messages (stdin to PTY)
   - Server → Client: Binary messages (stdout from PTY)
   - Either direction: Text messages for control (resize, exit code)
4. On process exit: Server sends JSON `{"exitCode": N}` and closes
```

### Control Messages (JSON over TextMessage)

```json
// Client → Server: Resize PTY
{"type": "resize", "rows": 24, "cols": 80}

// Server → Client: Exit notification (final message)
{"type": "exit", "exitCode": 0}

// Server → Client: Error
{"type": "error", "message": "process not found"}
```

### Data Messages (Binary)

- **Client → Server**: Raw bytes written to PTY stdin
- **Server → Client**: Raw bytes read from PTY stdout

## Implementation Plan

### Phase 1: WebSocket Infrastructure

Already available - the codebase uses `github.com/coder/websocket` which is already a dependency.

### Phase 2: Implement WebSocket Attach Handler ✓ DONE

Implemented `HandleProcessAttachWS` in `server/cmd/api/api/process.go`:
- Uses `github.com/coder/websocket` for WebSocket handling
- Binary messages for stdin/stdout data
- Text messages (JSON) for control messages (resize, exit)
- Serialized writes through a channel to avoid concurrent write issues
- Graceful shutdown coordination between PTY, WebSocket, and process exit

### Phase 3: Update Routing ✓ DONE

Updated `server/cmd/api/main.go` to use the new WebSocket handler directly.
The old HTTP hijack handler is preserved in the codebase but no longer routed to.

### Phase 4: Testing

1. **Unit tests** for WebSocket handler
2. **Integration tests** using a WebSocket client
3. **Test through proxy** to verify WebSocket works with intermediaries
4. **Test resize** messages work correctly

### Phase 5: Deprecation of Hijack (Optional)

1. Add deprecation warning to hijack path
2. Eventually remove hijack support

## Code Changes Summary

### Files to Modify

| File | Changes |
|------|---------|
| `server/go.mod` | Add `github.com/gorilla/websocket` |
| `server/cmd/api/api/process.go` | Add `HandleProcessAttachWS`, `wsReadWriter` |
| `server/cmd/api/main.go` | Route WebSocket upgrade to new handler |

### New Types

```go
// Control message types
type AttachControlMessage struct {
    Type     string `json:"type"`     // "resize", "exit", "error"
    Rows     int    `json:"rows,omitempty"`
    Cols     int    `json:"cols,omitempty"`
    ExitCode *int   `json:"exitCode,omitempty"`
    Message  string `json:"message,omitempty"`
}
```

## Comparison with hypeman

| Aspect | hypeman | kernel-images (proposed) |
|--------|---------|--------------------------|
| Transport | WebSocket | WebSocket |
| Backend | gRPC over vsock to guest agent | Local PTY file descriptor |
| First message | JSON with command | N/A (process already spawned) |
| Data messages | Binary stdin/stdout | Binary stdin/stdout |
| Resize | Part of ExecStart or separate? | Separate control message |
| Exit code | JSON in final message | JSON in final message |

### Key Difference

In hypeman, the exec command is initiated via the WebSocket connection. In kernel-images, the process is already spawned via `POST /process/spawn`, and the WebSocket attach is a separate step. This means:

1. No need for a "start" message - just connect and stream
2. Process ID is in the URL path, not in a message
3. Resize can happen via either the existing REST endpoint OR via WebSocket control message

## Alternative Considered: Keep Hijack, Add WebSocket Option

We could keep both:
- `GET /process/{id}/attach` - WebSocket (new, recommended)  
- `GET /process/{id}/attach/raw` - HTTP hijack (legacy)

This provides backward compatibility but adds maintenance burden.

## Recommendation

Implement WebSocket-only attach for new clients. The hijack approach is inherently problematic with proxies and should be phased out. Given that this is a relatively new feature, a clean break is acceptable.

## Timeline Estimate

- Phase 1 (WebSocket infra): 1 hour
- Phase 2 (Handler implementation): 2-3 hours  
- Phase 3 (Routing): 30 min
- Phase 4 (Testing): 2-3 hours
- Total: ~1 day

## Open Questions

1. **Should we support both WebSocket and hijack temporarily?**
   - Recommendation: No, just switch to WebSocket

2. **Should resize be via WebSocket control message or keep the REST endpoint?**
   - Recommendation: Support both for flexibility

3. **Should we add reconnection support?**
   - Recommendation: Not in v1, but WebSocket makes this possible in the future

4. **Do we need to update any SDK/CLI tools?**
   - Need to check if there are any clients using the attach endpoint
