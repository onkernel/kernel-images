package chromiumflags

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

// FlagsFile is the structured JSON representation stored at /chromium/flags.
//
// Example on disk:
// { "flags": ["--foo", "--bar=1"] }
type FlagsFile struct {
	Flags []string `json:"flags"`
}

// parseFlags splits a space-delimited string of Chromium flags into tokens.
// Tokens are expected in the form --flag or --flag=value. Quotes are not supported,
// matching the previous bash implementation which used simple word-splitting.
func parseFlags(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return []string{}
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

// ReadOptionalFlagFile returns the flags array from the JSON file at path.
// If the file does not exist, it returns nil and a nil error.
func ReadOptionalFlagFile(path string) ([]string, error) {
	// If the file doesn't exist, treat as empty overlay
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	// Read entire content to allow JSON detection
	content, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	b := strings.TrimSpace(string(content))
	if b == "" {
		return nil, nil
	}

	// format: JSON with { "flags": ["--flag", "--flag=val"] }
	var jf FlagsFile
	if err := json.Unmarshal([]byte(b), &jf); err != nil {
		return nil, err
	}
	if jf.Flags == nil {
		return nil, errors.New("flags file missing 'flags' array")
	}
	// Normalize tokens and return slice
	normalized := []string{}
	for _, tok := range jf.Flags {
		if t := strings.TrimSpace(tok); t != "" {
			normalized = append(normalized, t)
		}
	}
	return normalized, nil
}

// MergeFlags merges base flags with runtime flags, returning the final merged flags as a string.
// The merging logic respects extension-related flag semantics:
// 1) If runtime specifies --disable-extensions, it overrides everything extension related
// 2) Else if base specifies --disable-extensions and runtime does NOT specify any --load-extension, keep base disable
// 3) Else, build from merged load/except
// Non-extension flags from both base and runtime are combined with deduplication (first occurrence preserved).
func MergeFlags(baseTokens, runtimeTokens []string) []string {
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
	return final
}

// MergeFlagsWithRuntimeTokens merges base flags (string, e.g. from env CHROMIUM_FLAGS)
// with runtime token slice and returns final tokens.
func MergeFlagsWithRuntimeTokens(baseFlags string, runtimeTokens []string) []string {
	base := parseFlags(baseFlags)
	return MergeFlags(base, runtimeTokens)
}

// WriteFlagFile writes the provided tokens to the given path as JSON in the
// form: { "flags": ["--foo", "--bar=1"] } with file mode 0644.
// The function creates or truncates the file.
func WriteFlagFile(path string, tokens []string) error {
	// Normalize tokens: trim and drop empties
	normalized := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if s := strings.TrimSpace(t); s != "" {
			normalized = append(normalized, s)
		}
	}
	data, err := json.Marshal(FlagsFile{Flags: normalized})
	if err != nil {
		return err
	}
	// Ensure trailing newline for readability
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
