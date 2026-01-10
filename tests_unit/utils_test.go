//go:build tests_unit

package tests_unit

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/actionforge/actrun-cli/core"
	"github.com/actionforge/actrun-cli/utils"
)

func Test_If(t *testing.T) {

	r1 := utils.If(true, 1, 2)
	if r1 != 1 {
		t.Error("if(true, 1, 2) must be 1")
		return
	}

	r2 := utils.If(false, 1, 2)
	if r2 != 2 {
		t.Error("if(false, 1, 2) must be 2")
		return
	}

	r3 := utils.If(true, "a", "b")
	if r3 != "a" {
		t.Error("if(true, \"a\", \"b\") must be \"a\"")
		return
	}

	r4 := utils.If(false, "a", "b")
	if r4 != "b" {
		t.Error("if(false, \"a\", \"b\") must be \"b\"")
		return
	}
}

func Test_Error1(t *testing.T) {
	err := core.CreateErr(nil, nil, "this is the main error")
	errStr := fmt.Sprintf("%v\n", err)

	expected := "error:\n   1: this is the main error\n"

	if errStr != expected {
		t.Errorf("expected: %v, got: %v", expected, errStr)
		return
	}
}

func Test_Error2(t *testing.T) {
	err1 := core.CreateErr(nil, nil, "this is the main error")
	err2 := core.CreateErr(nil, err1, "this is the 2nd error")

	errStr := fmt.Sprintf("%v\n", err2)

	expected := "error:\n   1: this is the 2nd error\n       ↳ this is the main error\n"

	if errStr != expected {
		t.Errorf("expected: %v, got: %v", expected, errStr)
		return
	}
}

func Test_Error3(t *testing.T) {
	_, err := os.Stat("doesnt-exist")
	err1 := core.CreateErr(nil, err, "this is the main error").SetHint("this is a hint")
	err2 := core.CreateErr(nil, err1, "this is the 2nd error")

	errStr := fmt.Sprintf("%v\n", err2)

	var expected string
	if runtime.GOOS == "windows" {
		expected = "error:\n   1: this is the 2nd error\n       ↳ this is the main error\n        ↳ GetFileAttributesEx doesnt-exist: The system cannot find the file specified.\n\nhint:\n  this is a hint\n"
	} else {
		expected = "error:\n   1: this is the 2nd error\n       ↳ this is the main error\n        ↳ stat doesnt-exist: no such file or directory\n\nhint:\n  this is a hint\n"
	}

	if errStr != expected {
		t.Errorf("expected: %v, got: %v", expected, errStr)
		return
	}
}

// Test a graph that spits out an error object with an execution state attached
func Test_Error4(t *testing.T) {
	// The content of this graph doesn't matter.
	// We just need a graph that will trigger a runtime error.
	graphWithRuntimeError := `editor:
  version:
    created: v1.34.0
entry: start
type: generic
nodes:
  - id: start
    type: core/start@v1
    position:
      x: -170
      y: -50
  - id: storage-upload-v1-lemon-tiger-starfish
    type: core/storage-upload@v1
    position:
      x: 210
      y: -70
connections: []
executions:
  - src:
      node: start
      port: exec
    dst:
      node: storage-upload-v1-lemon-tiger-starfish
      port: exec
    isLoop: false
`

	err := core.RunGraph(context.Background(), "tests_unit/testdata/error4.yaml", []byte(graphWithRuntimeError), core.RunOpts{}, nil)
	if err == nil {
		t.Error("expected error")
		return
	}

	errStr := fmt.Sprintf("%v\n", err)

	expected := "error:\n   1: execute 'Start' (start)\n   2: execute 'Storage Upload' (storage-upload-v1-lemon-tiger-starfish)\n      no value for input 'Data' (data)\n\n\n\nhint:\n  No input value provided. Set a value or connect the input with a node\n"

	if errStr != expected {
		t.Errorf("expected: %v, got: %v", expected, errStr)
		return
	}
}

// Helper function to join paths using the platform-specific separator
func joinPaths(paths ...string) string {
	return strings.Join(paths, string(os.PathListSeparator))
}

// Helper function to generate a platform-independent path
func getPlatformPath(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(path, "/", "\\")
	}
	return path
}

// TestMergeEnvMapsOverwrite tests that values from src overwrite those from dst.
func TestMergeEnvMapsOverwrite(t *testing.T) {

	base := map[string]string{
		"USER": "user1",
		"HOME": getPlatformPath("/home/user1"),
	}

	overlay := map[string]string{
		"USER":  "user2", // Should overwrite USER from dst
		"SHELL": getPlatformPath("/bin/bash"),
	}

	expected := map[string]string{
		"USER":  "user2",
		"HOME":  getPlatformPath("/home/user1"),
		"SHELL": getPlatformPath("/bin/bash"),
	}

	result := utils.MergeEnvMaps(overlay, base)

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, but got %v", expected, result)
	}
}

// TestMergeEnvMapsPathMerge tests that PATH variables are merged with the correct separator.
func TestMergeEnvMapsPathMerge(t *testing.T) {
	base := map[string]string{
		"PATH": getPlatformPath("/opt/bin"),
	}

	target := map[string]string{
		"PATH": getPlatformPath("/usr/bin"),
	}

	expected := map[string]string{
		"PATH": joinPaths(getPlatformPath("/usr/bin"), getPlatformPath("/opt/bin")),
	}

	result := utils.MergeEnvMaps(target, base)

	if result["PATH"] != expected["PATH"] {
		t.Errorf("Expected PATH to be %v, but got %v", expected["PATH"], result["PATH"])
	}
}

// TestMergeEnvMapsPathAlreadyHasSeparator tests that when dst's PATH ends with a separator, no duplicate is added.
func TestMergeEnvMapsPathAlreadyHasSeparator(t *testing.T) {
	dst := map[string]string{
		"PATH": getPlatformPath("/usr/bin") + string(os.PathListSeparator),
	}

	src := map[string]string{
		"PATH": getPlatformPath("/opt/bin"),
	}

	expected := map[string]string{
		"PATH": joinPaths(getPlatformPath("/usr/bin"), getPlatformPath("/opt/bin")),
	}

	result := utils.MergeEnvMaps(dst, src)

	if result["PATH"] != expected["PATH"] {
		t.Errorf("Expected PATH to be %v, but got %v", expected["PATH"], result["PATH"])
	}
}

// TestMergeEnvMapsPathSrcHasSeparator tests that when src's PATH starts with a separator, no duplicate is added.
func TestMergeEnvMapsPathSrcHasSeparator(t *testing.T) {
	dst := map[string]string{
		"PATH": getPlatformPath("/usr/bin"),
	}

	src := map[string]string{
		"PATH": string(os.PathListSeparator) + getPlatformPath("/opt/bin"),
	}

	expected := map[string]string{
		"PATH": joinPaths(getPlatformPath("/usr/bin"), getPlatformPath("/opt/bin")),
	}

	result := utils.MergeEnvMaps(dst, src)

	if result["PATH"] != expected["PATH"] {
		t.Errorf("Expected PATH to be %v, but got %v", expected["PATH"], result["PATH"])
	}
}

// TestMergeEnvMapsEmptyDst tests that an empty dst map is properly populated by src.
func TestMergeEnvMapsEmptyDst(t *testing.T) {
	dst := map[string]string{}

	src := map[string]string{
		"USER":  "user2",
		"PATH":  getPlatformPath("/opt/bin"),
		"SHELL": getPlatformPath("/bin/bash"),
	}

	expected := map[string]string{
		"USER":  "user2",
		"PATH":  getPlatformPath("/opt/bin"),
		"SHELL": getPlatformPath("/bin/bash"),
	}

	result := utils.MergeEnvMaps(dst, src)

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, but got %v", expected, result)
	}
}

// TestMergeEnvMapsEmptySrc tests that when src is empty, dst remains unchanged.
func TestMergeEnvMapsEmptySrc(t *testing.T) {
	dst := map[string]string{
		"USER":  "user1",
		"PATH":  getPlatformPath("/usr/bin"),
		"SHELL": getPlatformPath("/bin/sh"),
	}

	src := map[string]string{}

	expected := map[string]string{
		"USER":  "user1",
		"PATH":  getPlatformPath("/usr/bin"),
		"SHELL": getPlatformPath("/bin/sh"),
	}

	result := utils.MergeEnvMaps(dst, src)

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, but got %v", expected, result)
	}
}

// TestMergeEnvMapsEmptyPath tests merging when one PATH is empty in dst.
func TestMergeEnvMapsEmptyPath(t *testing.T) {
	dst := map[string]string{
		"PATH": "",
	}

	src := map[string]string{
		"PATH": getPlatformPath("/opt/bin"),
	}

	expected := map[string]string{
		"PATH": getPlatformPath("/opt/bin"),
	}

	result := utils.MergeEnvMaps(dst, src)

	if result["PATH"] != expected["PATH"] {
		t.Errorf("Expected PATH to be %v, but got %v", expected["PATH"], result["PATH"])
	}
}

// TestMergeEnvMapsWithNoPath tests merging maps without any PATH key.
func TestMergeEnvMapsWithNoPath(t *testing.T) {
	dst := map[string]string{
		"USER": "user1",
	}

	src := map[string]string{
		"SHELL": getPlatformPath("/bin/bash"),
	}

	expected := map[string]string{
		"USER":  "user1",
		"SHELL": getPlatformPath("/bin/bash"),
	}

	result := utils.MergeEnvMaps(dst, src)

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, but got %v", expected, result)
	}
}

// TestMergeEnvMapsBothPathsEmpty tests when both PATH values are empty.
func TestMergeEnvMapsBothPathsEmpty(t *testing.T) {
	dst := map[string]string{
		"PATH": "",
	}

	src := map[string]string{
		"PATH": "",
	}

	expected := map[string]string{
		"PATH": "",
	}

	result := utils.MergeEnvMaps(dst, src)

	if result["PATH"] != expected["PATH"] {
		t.Errorf("Expected PATH to be %v, but got %v", expected["PATH"], result["PATH"])
	}
}

// TestMergeEnvMapsSamePath tests when both maps have the same PATH value.
func TestMergeEnvMapsSamePath(t *testing.T) {
	dst := map[string]string{
		"PATH": getPlatformPath("/usr/bin"),
	}

	src := map[string]string{
		"PATH": getPlatformPath("/usr/bin"),
	}

	expected := map[string]string{
		"PATH": getPlatformPath("/usr/bin"),
	}

	result := utils.MergeEnvMaps(dst, src)

	if result["PATH"] != expected["PATH"] {
		t.Errorf("Expected PATH to be %v, but got %v", expected["PATH"], result["PATH"])
	}
}

// TestMergeEnvMapsDifferentKeys tests when both maps have completely different keys.
func TestMergeEnvMapsDifferentKeys(t *testing.T) {
	dst := map[string]string{
		"USER":  "user1",
		"SHELL": getPlatformPath("/bin/sh"),
	}

	src := map[string]string{
		"HOME": getPlatformPath("/home/user1"),
		"PATH": getPlatformPath("/usr/bin"),
	}

	expected := map[string]string{
		"USER":  "user1",
		"SHELL": getPlatformPath("/bin/sh"),
		"HOME":  getPlatformPath("/home/user1"),
		"PATH":  getPlatformPath("/usr/bin"),
	}

	result := utils.MergeEnvMaps(dst, src)

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, but got %v", expected, result)
	}
}
