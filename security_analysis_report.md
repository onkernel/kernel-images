# Security Analysis Report - Server Directory

## Executive Summary

This report analyzes the `/server` directory codebase for potential security vulnerabilities and bugs. The server implements a screen recording API using FFmpeg with Go. While the code shows good practices in many areas, several security concerns and potential bugs have been identified.

## Critical Security Issues

### 1. **Command Injection via FFmpeg Binary Path** 🔴 **HIGH RISK**

**Location**: `server/lib/recorder/ffmpeg.go:139`
```go
cmd := exec.Command(fr.binaryPath, args...)
```

**Issue**: The `binaryPath` field can be controlled via the `FFMPEG_PATH` environment variable and is passed directly to `exec.Command` without validation.

**Risk**: An attacker who can control environment variables could execute arbitrary commands by setting `FFMPEG_PATH` to malicious values like:
- `FFMPEG_PATH="/bin/sh -c 'malicious_command' #"`
- `FFMPEG_PATH="ffmpeg; rm -rf /"`

**Recommendation**: 
- Validate that `binaryPath` contains only allowed characters and paths
- Use absolute paths only
- Consider allowlisting specific ffmpeg binary locations

### 2. **Path Traversal in Output Directory** 🔴 **HIGH RISK**

**Location**: `server/lib/recorder/ffmpeg.go:84`
```go
outputPath: filepath.Join(*mergedParams.OutputDir, fmt.Sprintf("%s.mp4", id)),
```

**Issue**: The `OutputDir` can be controlled via the `OUTPUT_DIR` environment variable, and the recorder `id` is user-controlled (hardcoded as "main" but could be changed).

**Risk**: Potential path traversal allowing file writes outside intended directory:
- `OUTPUT_DIR="../../../etc"` could write files to system directories
- Malicious `id` values with path separators could escape the directory

**Recommendation**:
- Validate and sanitize `OutputDir` to prevent path traversal
- Ensure `id` contains only safe characters (alphanumeric, dash, underscore)
- Use `filepath.Clean()` and validate the final path is within expected boundaries

### 3. **Resource Exhaustion - No HTTP Server Timeouts** 🟡 **MEDIUM RISK**

**Location**: `server/cmd/api/main.go:86-89`
```go
srv := &http.Server{
    Addr:    fmt.Sprintf(":%d", config.Port),
    Handler: r,
}
```

**Issue**: HTTP server lacks timeout configurations, making it vulnerable to slowloris and similar DoS attacks.

**Recommendation**: Add timeout configurations:
```go
srv := &http.Server{
    Addr:           fmt.Sprintf(":%d", config.Port),
    Handler:        r,
    ReadTimeout:    10 * time.Second,
    WriteTimeout:   10 * time.Second,
    IdleTimeout:    60 * time.Second,
    MaxHeaderBytes: 1 << 20, // 1 MB
}
```

## Medium Risk Issues

### 4. **Insufficient Input Validation** 🟡 **MEDIUM RISK**

**Location**: `server/cmd/config/config.go:29-48`

**Issues**:
- `DisplayNum` validation only checks `< 0` but doesn't validate upper bounds
- `FrameRate` validation allows 0, which could cause issues
- `MaxSizeInMB` allows 0, which could cause issues
- No validation of realistic upper bounds

**Recommendation**: Add proper bounds checking:
```go
if config.DisplayNum < 0 || config.DisplayNum > 99 {
    return fmt.Errorf("DISPLAY_NUM must be between 0 and 99")
}
if config.FrameRate <= 0 || config.FrameRate > 120 {
    return fmt.Errorf("FRAME_RATE must be between 1 and 120")
}
if config.MaxSizeInMB <= 0 || config.MaxSizeInMB > 10000 {
    return fmt.Errorf("MAX_SIZE_MB must be between 1 and 10000")
}
```

### 5. **Race Condition in Recorder State** 🟡 **MEDIUM RISK**

**Location**: `server/lib/recorder/ffmpeg.go:159-179`

**Issue**: The `IsRecording()` check and subsequent operations are not atomic, creating a race condition window.

**Example vulnerable sequence**:
```go
// In API layer
if rec.IsRecording(ctx) { // Check 1
    return conflict_error
}
// Race condition window here
rec.Start(ctx) // Could fail if another goroutine started recording
```

**Recommendation**: Use atomic operations or ensure the recorder manager handles concurrent access properly.

### 6. **Sensitive Information Logging** 🟡 **MEDIUM RISK**

**Location**: `server/cmd/api/main.go:35`
```go
slogger.Info("server configuration", "config", config)
```

**Issue**: Full configuration is logged, potentially exposing sensitive paths and settings.

**Recommendation**: Create a sanitized version of config for logging or log individual non-sensitive fields.

## Low Risk Issues

### 7. **Missing Content-Type Validation** 🟢 **LOW RISK**

**Location**: API endpoints don't validate Content-Type headers

**Issue**: Endpoints accept any Content-Type, potentially leading to confusion or bypassing security controls.

**Recommendation**: Validate Content-Type for POST endpoints expecting JSON.

### 8. **No Rate Limiting** 🟢 **LOW RISK**

**Issue**: No rate limiting on API endpoints could allow abuse.

**Recommendation**: Implement rate limiting middleware, especially for resource-intensive operations like starting recordings.

### 9. **Process Group Handling** 🟢 **LOW RISK**

**Location**: `server/lib/recorder/ffmpeg.go:141`
```go
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
```

**Issue**: While this is good practice, the error handling in `shutdownInPhases` ignores signal errors, which could leave zombie processes.

**Recommendation**: Add logging for signal errors to help with debugging.

## Positive Security Practices

✅ **Good practices observed:**
- Proper graceful shutdown implementation
- Context-based cancellation
- Process group management for signal handling
- Input validation in OpenAPI spec with min/max constraints
- Structured logging
- Error handling and proper HTTP status codes
- Use of structured configuration with environment variables
- Thread-safe recorder management with mutexes

## Recommendations Summary

### Immediate Actions (High Priority)
1. **Fix command injection**: Validate `FFMPEG_PATH` environment variable
2. **Fix path traversal**: Sanitize `OUTPUT_DIR` and recorder IDs
3. **Add HTTP timeouts**: Configure server timeouts to prevent DoS

### Short-term Actions (Medium Priority)
4. Improve input validation with realistic bounds
5. Fix race conditions in recorder state management
6. Sanitize logging of sensitive configuration

### Long-term Actions (Low Priority)
7. Add rate limiting
8. Implement Content-Type validation
9. Enhance process cleanup error handling

## Testing Recommendations

- Add security-focused unit tests for path traversal scenarios
- Test with malicious environment variables
- Load testing to verify timeout configurations
- Concurrent access testing for race conditions

## Conclusion

The codebase demonstrates good engineering practices but contains several security vulnerabilities that should be addressed before production deployment. The command injection and path traversal issues are the most critical and should be fixed immediately.