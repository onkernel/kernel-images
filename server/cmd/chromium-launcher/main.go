package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// parseFlags splits a space-delimited string of Chromium flags into tokens.
// Tokens are expected in the form --flag or --flag=value. Quotes are not supported,
// matching the previous bash implementation which used simple word-splitting.
func parseFlags(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	return strings.Fields(input)
}

// appendCSVInto appends comma-separated values into dst, skipping empty items.
func appendCSVInto(dst *[]string, csv string) {
	for _, part := range strings.Split(csv, ",") {
		if p := strings.TrimSpace(part); p != "" {
			*dst = append(*dst, p)
		}
	}
}

// parseTokenStream extracts extension-related flags and collects non-extension flags.
// It returns the list of non-extension tokens and, via references, fills the buckets for
// --load-extension, --disable-extensions-except and a possible --disable-extensions token for that stream.
func parseTokenStream(tokens []string, load, except *[]string, disableAll *string) (nonExt []string) {
	for _, tok := range tokens {
		switch {
		case strings.HasPrefix(tok, "--load-extension="):
			val := strings.TrimPrefix(tok, "--load-extension=")
			appendCSVInto(load, val)
		case strings.HasPrefix(tok, "--disable-extensions-except="):
			val := strings.TrimPrefix(tok, "--disable-extensions-except=")
			appendCSVInto(except, val)
		case tok == "--disable-extensions":
			*disableAll = tok
		default:
			nonExt = append(nonExt, tok)
		}
	}
	return nonExt
}

// union merges two lists of strings, returning a new list with duplicates removed.
func union(base, rt []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, v := range append(append([]string{}, base...), rt...) {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// readOptionalFlagFile returns the file contents with newlines collapsed to single spaces.
// If the file does not exist, it returns an empty string and a nil error.
// Intended for space-delimited Chromium flag overlays.
func readOptionalFlagFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	var b strings.Builder
	s := bufio.NewScanner(f)
	for s.Scan() {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(s.Text())
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	return b.String(), nil
}

func main() {
	headless := flag.Bool("headless", false, "Run Chromium with headless flags")
	chromiumPath := flag.String("chromium", "chromium", "Chromium binary path (default: chromium)")
	runtimeFlagsPath := flag.String("runtime-flags", "/chromium/flags", "Path to runtime flags overlay file")
	flag.Parse()

	// Inputs
	internalPort := strings.TrimSpace(os.Getenv("INTERNAL_PORT"))
	if internalPort == "" {
		internalPort = "9223"
	}
	baseFlags := os.Getenv("CHROMIUM_FLAGS")
	runtimeFlags, err := readOptionalFlagFile(*runtimeFlagsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed reading runtime flags: %v\n", err)
		os.Exit(1)
	}

	// Tokenize
	baseTokens := parseFlags(baseFlags)
	runtimeTokens := parseFlags(runtimeFlags)

	// Buckets
	var (
		baseNonExt     []string // Non-extension related flags contained in base
		runtimeNonExt  []string // Non-extension related flags contained in runtime
		baseLoad       []string // --load-extension flags contained in base
		baseExcept     []string // --disable-extensions-except flags for base
		rtLoad         []string // --load-extension flags contained in runtime
		rtExcept       []string // --disable-extensions-except flags contained in runtime
		baseDisableAll string   // --disable-extensions flag contained in base
		rtDisableAll   string   // --disable-extensions flag contained in runtime
	)

	baseNonExt = parseTokenStream(baseTokens, &baseLoad, &baseExcept, &baseDisableAll)
	runtimeNonExt = parseTokenStream(runtimeTokens, &rtLoad, &rtExcept, &rtDisableAll)

	// Merge extension lists
	mergedLoad := union(baseLoad, rtLoad)
	mergedExcept := union(baseExcept, rtExcept)

	// Construct final extension-related flags respecting override semantics:
	// 1) If runtime specifies --disable-extensions, it overrides everything extension related
	// 2) Else if base specifies --disable-extensions and runtime does NOT specify any --load-extension, keep base disable
	// 3) Else, build from merged load/except
	var extFlags []string
	if rtDisableAll != "" {
		extFlags = append(extFlags, rtDisableAll)
	} else {
		if baseDisableAll != "" && len(rtLoad) == 0 {
			extFlags = append(extFlags, baseDisableAll)
		} else if len(mergedLoad) > 0 {
			extFlags = append(extFlags, "--load-extension="+strings.Join(mergedLoad, ","))
		}
		if len(mergedExcept) > 0 {
			extFlags = append(extFlags, "--disable-extensions-except="+strings.Join(mergedExcept, ","))
		}
	}

	// Combine and dedupe (preserving first occurrence)
	combined := append(append([]string{}, baseNonExt...), runtimeNonExt...)
	combined = append(combined, extFlags...)
	seen := make(map[string]struct{}, len(combined))
	final := make([]string, 0, len(combined))
	for _, tok := range combined {
		if tok == "" {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		final = append(final, tok)
	}
	finalFlagsJoined := strings.Join(final, " ")

	// Diagnostics for parity with previous scripts
	fmt.Printf("BASE_FLAGS: %s\n", baseFlags)
	fmt.Printf("RUNTIME_FLAGS: %s\n", runtimeFlags)
	fmt.Printf("FINAL_FLAGS: %s\n", finalFlagsJoined)

	// Common Chromium arguments
	chromiumArgs := []string{
		fmt.Sprintf("--remote-debugging-port=%s", internalPort),
		"--user-data-dir=/home/kernel/user-data",
		"--password-store=basic",
		"--no-first-run",
	}
	if *headless {
		chromiumArgs = append([]string{"--headless=new", "--remote-allow-origins=*"}, chromiumArgs...)
	}
	chromiumArgs = append(chromiumArgs, final...)

	runAsRoot := strings.EqualFold(strings.TrimSpace(os.Getenv("RUN_AS_ROOT")), "true")

	// Prepare environment
	env := os.Environ()
	env = append(env,
		"DISPLAY=:1",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/dbus/system_bus_socket",
	)

	if runAsRoot {
		// Replace current process with Chromium
		if p, err := execLookPath(*chromiumPath); err == nil {
			if err := syscall.Exec(p, append([]string{filepath.Base(p)}, chromiumArgs...), env); err != nil {
				fmt.Fprintf(os.Stderr, "exec chromium failed: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Fprintf(os.Stderr, "chromium binary not found: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Not running as root: call runuser to exec as kernel user, providing env vars inside
	runuserPath, err := execLookPath("runuser")
	if err != nil {
		fmt.Fprintf(os.Stderr, "runuser not found: %v\n", err)
		os.Exit(1)
	}

	// Build: runuser -u kernel -- env DISPLAY=... DBUS_... XDG_... HOME=... chromium <args>
	inner := []string{
		"env",
		"DISPLAY=:1",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/dbus/system_bus_socket",
		"XDG_CONFIG_HOME=/home/kernel/.config",
		"XDG_CACHE_HOME=/home/kernel/.cache",
		"HOME=/home/kernel",
		*chromiumPath,
	}
	inner = append(inner, chromiumArgs...)
	argv := append([]string{filepath.Base(runuserPath), "-u", "kernel", "--"}, inner...)
	if err := syscall.Exec(runuserPath, argv, env); err != nil {
		fmt.Fprintf(os.Stderr, "exec runuser failed: %v\n", err)
		os.Exit(1)
	}
}

// execLookPath helps satisfy syscall.Exec's requirement to pass an absolute path.
func execLookPath(file string) (string, error) {
	if strings.ContainsRune(file, os.PathSeparator) {
		return file, nil
	}
	return exec.LookPath(file)
}
