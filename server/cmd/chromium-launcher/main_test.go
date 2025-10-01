package main

import (
	"os"
	"os/exec"
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
	// Non-existent returns empty string and nil error
	if s, err := readOptionalFlagFile(filepath.Join(t.TempDir(), "not-there")); err != nil || s != "" {
		t.Fatalf("expected empty string and nil error for missing file, got %q, err=%v", s, err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "flags.txt")
	content := "line1\nline two with spaces\nline3"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	got, err := readOptionalFlagFile(path)
	if err != nil {
		t.Fatalf("readOptionalFlagFile error: %v", err)
	}
	want := "line1 line two with spaces line3"
	if got != want {
		t.Fatalf("readOptionalFlagFile content mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestLookPathAndExecLookPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mybin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	oldPath := os.Getenv("PATH")
	defer func() { _ = os.Setenv("PATH", oldPath) }()
	if err := os.Setenv("PATH", dir); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	// lookPath should find by PATH
	if p, err := exec.LookPath("mybin"); err != nil || p != bin {
		t.Fatalf("lookPath failed: p=%q err=%v", p, err)
	}

	// execLookPath should return input when absolute
	if p, err := execLookPath(bin); err != nil || p != bin {
		t.Fatalf("execLookPath absolute failed: p=%q err=%v", p, err)
	}

	// execLookPath should resolve by PATH for bare names
	if p, err := execLookPath("mybin"); err != nil || p != bin {
		t.Fatalf("execLookPath PATH search failed: p=%q err=%v", p, err)
	}
}
