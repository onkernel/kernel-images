package chromiumflags

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseFlags(t *testing.T) {
	// Empty input returns nil
	if got := parseFlags(""); got != nil {
		t.Fatalf("expected nil for empty input, got: %#v", got)
	}

	input := "  --foo --bar=1\t--baz  "
	got := parseFlags(input)
	want := []string{"--foo", "--bar=1", "--baz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseFlags mismatch:\n got: %#v\nwant: %#v", got, want)
	}

	// Quotes are not supported; ensure simple word splitting occurs
	input = `--flag="with space" --qux`
	got = parseFlags(input)
	if len(got) != 3 {
		t.Fatalf("expected 3 tokens due to simple splitting, got %d: %#v", len(got), got)
	}
}

func TestAppendCSVInto(t *testing.T) {
	var dst []string
	appendCSVInto(&dst, "a,, b , c,")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(dst, want) {
		t.Fatalf("appendCSVInto mismatch:\n got: %#v\nwant: %#v", dst, want)
	}
}

func TestParseTokenStream_BaseAndRuntime(t *testing.T) {
	var (
		baseLoad    []string
		baseExcept  []string
		rtLoad      []string
		rtExcept    []string
		baseDisable string
		rtDisable   string
	)

	baseTokens := []string{
		"--load-extension=/e1,/e2",
		"--disable-extensions-except=/x1",
		"--other=1",
		"--disable-extensions",
	}
	runtimeTokens := []string{
		"--disable-extensions-except=/x2,/x3",
		"--load-extension=/e3",
		"--disable-extensions",
		"--foo",
	}

	baseNonExt := parseTokenStream(baseTokens, &baseLoad, &baseExcept, &baseDisable)
	runtimeNonExt := parseTokenStream(runtimeTokens, &rtLoad, &rtExcept, &rtDisable)

	if !reflect.DeepEqual(baseLoad, []string{"/e1", "/e2"}) {
		t.Fatalf("base load-extension parsed incorrectly: %#v", baseLoad)
	}
	if !reflect.DeepEqual(baseExcept, []string{"/x1"}) {
		t.Fatalf("base disable-extensions-except parsed incorrectly: %#v", baseExcept)
	}
	if !reflect.DeepEqual(rtLoad, []string{"/e3"}) {
		t.Fatalf("runtime load-extension parsed incorrectly: %#v", rtLoad)
	}
	if !reflect.DeepEqual(rtExcept, []string{"/x2", "/x3"}) {
		t.Fatalf("runtime disable-extensions-except parsed incorrectly: %#v", rtExcept)
	}
	if baseDisable != "--disable-extensions" {
		t.Fatalf("expected base disable-all captured, got %q", baseDisable)
	}
	if rtDisable != "--disable-extensions" {
		t.Fatalf("expected runtime disable-all captured, got %q", rtDisable)
	}
	if !reflect.DeepEqual(baseNonExt, []string{"--other=1"}) {
		t.Fatalf("unexpected base non-extension tokens: %#v", baseNonExt)
	}
	if !reflect.DeepEqual(runtimeNonExt, []string{"--foo"}) {
		t.Fatalf("unexpected runtime non-extension tokens: %#v", runtimeNonExt)
	}
}

func TestMergeUnion(t *testing.T) {
	base := []string{"a", "b", "a", ""}
	rt := []string{"b", "c", "", "a"}
	got := union(base, rt)
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeUnion mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestOverrideSemantics_DisableBase_LoadRuntime(t *testing.T) {
	// Base has --disable-extensions, runtime has --load-extension → runtime overrides, no disable-all in final
	baseFlags := "--disable-extensions"
	runtimeFlags := "--load-extension=/e1"

	baseTokens := parseFlags(baseFlags)
	runtimeTokens := parseFlags(runtimeFlags)

	var (
		baseLoad    []string
		baseExcept  []string
		rtLoad      []string
		rtExcept    []string
		baseDisable string
		rtDisable   string
	)

	_ = parseTokenStream(baseTokens, &baseLoad, &baseExcept, &baseDisable)
	_ = parseTokenStream(runtimeTokens, &rtLoad, &rtExcept, &rtDisable)

	mergedLoad := union(baseLoad, rtLoad)
	mergedExcept := union(baseExcept, rtExcept)

	var extFlags []string
	if rtDisable != "" {
		extFlags = append(extFlags, rtDisable)
	} else {
		if baseDisable != "" && len(rtLoad) == 0 {
			extFlags = append(extFlags, baseDisable)
		} else if len(mergedLoad) > 0 {
			extFlags = append(extFlags, "--load-extension="+strings.Join(mergedLoad, ","))
		}
		if len(mergedExcept) > 0 {
			extFlags = append(extFlags, "--disable-extensions-except="+strings.Join(mergedExcept, ","))
		}
	}

	for _, f := range extFlags {
		if f == "--disable-extensions" {
			t.Fatalf("unexpected disable-all in final flags when runtime loads extensions: %#v", extFlags)
		}
	}
}

func TestOverrideSemantics_DisableRuntime_Wins(t *testing.T) {
	// Runtime has --disable-extensions → overrides everything extension related
	baseFlags := "--load-extension=/e1 --disable-extensions-except=/x1"
	runtimeFlags := "--disable-extensions"

	baseTokens := parseFlags(baseFlags)
	runtimeTokens := parseFlags(runtimeFlags)

	var (
		baseLoad       []string
		baseExcept     []string
		rtLoad         []string
		rtExcept       []string
		baseDisable    string
		runtimeDisable string
	)

	_ = parseTokenStream(baseTokens, &baseLoad, &baseExcept, &baseDisable)
	_ = parseTokenStream(runtimeTokens, &rtLoad, &rtExcept, &runtimeDisable)

	var extFlags []string
	if runtimeDisable != "" {
		extFlags = append(extFlags, runtimeDisable)
	}

	if len(extFlags) != 1 || extFlags[0] != "--disable-extensions" {
		t.Fatalf("runtime disable should win exclusively, got: %#v", extFlags)
	}
}

func TestReadOptionalFlagFile(t *testing.T) {
	// Non-existent returns nil slice and nil error
	if s, err := ReadOptionalFlagFile(filepath.Join(t.TempDir(), "not-there")); err != nil || s != nil {
		t.Fatalf("expected nil slice and nil error for missing file, got %#v, err=%v", s, err)
	}

	// Plain text is no longer supported: expect an error
	dir := t.TempDir()
	path := filepath.Join(dir, "flags.txt")
	content := "--foo\n--bar=1"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if _, err := ReadOptionalFlagFile(path); err == nil {
		t.Fatalf("expected error for plain text flags file, got nil")
	}
}

func TestReadOptionalFlagFile_JSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flags.json")
	content := `{"flags":["--one","--two=2","  ","--three"]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	got, err := ReadOptionalFlagFile(path)
	if err != nil {
		t.Fatalf("ReadOptionalFlagFile error: %v", err)
	}
	want := []string{"--one", "--two=2", "--three"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadOptionalFlagFile(JSON) content mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestWriteFlagFileAndReadBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flags.json")
	tokens := []string{" --a ", "", "--b=1"}
	if err := WriteFlagFile(path, tokens); err != nil {
		t.Fatalf("WriteFlagFile error: %v", err)
	}
	// Read as runtime flags (tokens)
	got, err := ReadOptionalFlagFile(path)
	if err != nil {
		t.Fatalf("ReadOptionalFlagFile error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"--a", "--b=1"}) {
		t.Fatalf("unexpected merged runtime tokens: %#v", got)
	}
	// Validate JSON structure in file
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	var jf FlagsFile
	if err := json.Unmarshal(raw, &jf); err != nil {
		t.Fatalf("json unmarshal error: %v; content=%s", err, string(raw))
	}
	if !reflect.DeepEqual(jf.Flags, []string{"--a", "--b=1"}) {
		t.Fatalf("unexpected flags in file: %#v", jf.Flags)
	}
}

// TestWriteFlagFileFromString removed: callers should use WriteFlagFile with tokens.

func TestMergeFlags(t *testing.T) {
	tests := []struct {
		name         string
		baseFlags    string
		runtimeFlags string
		want         string
	}{
		{
			name:         "empty base and runtime",
			baseFlags:    "",
			runtimeFlags: "",
			want:         "",
		},
		{
			name:         "base only, no runtime",
			baseFlags:    "--foo --bar=1",
			runtimeFlags: "",
			want:         "--foo --bar=1",
		},
		{
			name:         "runtime only, no base",
			baseFlags:    "",
			runtimeFlags: "--foo --bar=1",
			want:         "--foo --bar=1",
		},
		{
			name:         "merge non-extension flags",
			baseFlags:    "--foo --bar=1",
			runtimeFlags: "--baz --qux=2",
			want:         "--foo --bar=1 --baz --qux=2",
		},
		{
			name:         "deduplicate non-extension flags",
			baseFlags:    "--foo --bar=1",
			runtimeFlags: "--foo --baz",
			want:         "--foo --bar=1 --baz",
		},
		{
			name:         "merge load-extension flags",
			baseFlags:    "--load-extension=/e1",
			runtimeFlags: "--load-extension=/e2",
			want:         "--load-extension=/e1,/e2",
		},
		{
			name:         "merge disable-extensions-except flags",
			baseFlags:    "--disable-extensions-except=/x1",
			runtimeFlags: "--disable-extensions-except=/x2",
			want:         "--disable-extensions-except=/x1,/x2",
		},
		{
			name:         "runtime disable-extensions overrides all",
			baseFlags:    "--load-extension=/e1 --disable-extensions-except=/x1",
			runtimeFlags: "--disable-extensions",
			want:         "--disable-extensions",
		},
		{
			name:         "base disable-extensions, runtime load-extension overrides",
			baseFlags:    "--disable-extensions",
			runtimeFlags: "--load-extension=/e1",
			want:         "--load-extension=/e1",
		},
		{
			name:         "base disable-extensions, no runtime load-extension keeps disable",
			baseFlags:    "--disable-extensions --other=1",
			runtimeFlags: "--foo",
			want:         "--other=1 --foo --disable-extensions",
		},
		{
			name:         "complex merge with extensions and non-extensions",
			baseFlags:    "--foo --load-extension=/e1 --disable-extensions-except=/x1",
			runtimeFlags: "--bar --load-extension=/e2 --disable-extensions-except=/x2",
			want:         "--foo --bar --load-extension=/e1,/e2 --disable-extensions-except=/x1,/x2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeFlags(parseFlags(tt.baseFlags), parseFlags(tt.runtimeFlags))
			wantTokens := parseFlags(tt.want)
			if !reflect.DeepEqual(got, wantTokens) {
				t.Errorf("MergeFlags() mismatch:\n got: %#v\nwant: %#v", got, wantTokens)
			}
		})
	}
}
