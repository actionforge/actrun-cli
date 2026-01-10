package utils

import (
	"cmp"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"maps"
	"os"
	"os/exec"
	"reflect"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/exp/constraints"
)

var (
	ORIGIN_ENV_SHELL  = "env (shell)"
	ORIGIN_ENV_DOTENV = "env (dotenv)"
)

func SafeCloseReaderAndIgnoreError(r io.Reader) {
	_ = SafeCloseReader(r)
}

func SafeCloseReader(r io.Reader) error {
	closer, ok := r.(io.Closer)
	if ok {
		return closer.Close()
	}
	return nil
}

// Inline-if alternative in Go. Example:
// e ? a : b becomes If(e, a, b)
func If[E bool, T any](exp E, a T, b T) T {
	if exp {
		return a
	} else {
		return b
	}
}

func NormalizeLineEndings(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func InitMapAndSliceInStructRecursively(v reflect.Value) {
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)

		if !field.CanSet() {
			continue
		}

		k := field.Kind()

		if k == reflect.Struct {
			InitMapAndSliceInStructRecursively(field)
			continue
		}

		if k == reflect.Map && field.IsNil() {
			fieldType := field.Type()
			newMap := reflect.MakeMap(fieldType)

			field.Set(newMap)
		} else if k == reflect.Slice && field.IsNil() {
			fieldType := field.Type()
			newSlice := reflect.MakeSlice(fieldType, 0, 0)

			field.Set(newSlice)
		}
	}
}

type ResolveCliParamOpts struct {
	Flag      bool
	Env       bool
	Optional  bool
	FlagValue string
	ActPrefix bool
}

type EnvKV struct {
	Value      string
	DotEnvFile bool
}

func GetShellEnvMapCopy() map[string]string {
	envs := map[string]string{}
	for _, e := range os.Environ() {
		envName, envValue, found := strings.Cut(e, "=")
		if found {
			// in some rare environments we got 'Path' instead of 'PATH'
			if envName == "Path" {
				envName = "PATH"
			}

			envs[envName] = envValue
		}
	}
	return envs
}

func GetAllEnvMapCopy() map[string]EnvKV {
	envs := map[string]EnvKV{}
	for k, dotV := range GetShellEnvMapCopy() {
		v, dotEnvSource := getEnvValue(k, "")
		if dotEnvSource {
			dotV = v
		}
		envs[k] = EnvKV{
			Value:      dotV,
			DotEnvFile: dotEnvSource,
		}
	}
	return envs
}

func ResolveCliParam(name string, opts ResolveCliParamOpts) (string, string) {

	var configValue string
	var resolvedSource string

	LogOut.Debugf("looking for value: '%s'\n", name)

	if opts.Env {
		envName := name
		if opts.ActPrefix {
			envName = "ACT_" + name
		}
		v, dotEnvSource := getEnvValue(strings.ToUpper(envName), "")
		if v != "" {
			valueSource := If(dotEnvSource, ORIGIN_ENV_DOTENV, ORIGIN_ENV_SHELL)
			LogOut.Debugf("  found value in: '%s'\n", valueSource)
			configValue = v
			resolvedSource = valueSource
		}
	}

	if opts.Flag && opts.FlagValue != "" {
		LogOut.Debug("  found value in flags\n")
		configValue = opts.FlagValue
		resolvedSource = "flag"
	}

	if configValue != "" {
		// redact sensitive values
		dbgName := strings.ToLower(name)
		dbgValue := configValue
		sensitive := []string{
			"secret",
			"token",
			"key",
			"password",
			"passphrase",
		}
		for _, s := range sensitive {
			if strings.Contains(dbgName, s) {
				dbgValue = "********"
			}
		}

		LogOut.Debugf("  evaluated to: '%s'\n", dbgValue)
	}

	if configValue == "" {
		if opts.Optional {
			LogOut.Debugf("  no value (is optional) found for: '%s'\n", name)
		} else {
			log.Panicf("no value for '%s' provided", name)
		}
	}

	return configValue, resolvedSource
}

func FindProjectRoot() string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.CombinedOutput()
	if err != nil {
		panic(err)
	}
	return strings.Trim(string(output), " \r\n")
}

func GetSha256OfBytes(data []byte) (string, error) {
	h := sha256.New()
	_, err := h.Write(data)
	if err != nil {
		return "", err
	}

	bs := h.Sum(nil)
	return fmt.Sprintf("%x\n", bs), nil
}

func GetSha256OfFile(filePath string) (string, error) {
	fc, err := os.ReadFile(filePath)
	if err != nil {
		return "", errors.Wrapf(err, "unable to determine sha256 of file: '%s'", filePath)
	}
	return GetSha256OfBytes(fc)
}

func Max[T constraints.Ordered](args ...T) T {
	if len(args) == 0 {
		return *new(T) // zero value of T
	}

	if isNan(args[0]) {
		return args[0]
	}

	max := args[0]
	for _, arg := range args[1:] {

		if isNan(arg) {
			return arg
		}

		if arg > max {
			max = arg
		}
	}
	return max
}

func isNan[T cmp.Ordered](arg T) bool {
	return arg != arg
}

/**
 * Merge two environment maps. If the same key exists in both maps,
 * the value from the src map will be used. The key "PATH" is
 * special and will be merged.
 */
func MergeEnvMaps(overlay map[string]string, base map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(overlay))
	maps.Copy(out, base)

	// the overlay on top
	for key, value := range overlay {
		if key == "PATH" {
			if existing, ok := out[key]; ok && existing != "" {
				out[key] = joinPaths(value, existing)
				continue
			}
		}

		// overlay overwrites base
		out[key] = value
	}

	return out
}

// joinPaths handles the splitting, deduplicating, and joining of the PATH varilabe.
func joinPaths(highPriority, lowPriority string) string {
	sep := string(os.PathListSeparator)
	newPaths := strings.Split(highPriority, sep)
	oldPaths := strings.Split(lowPriority, sep)

	finalPaths := make([]string, 0, len(newPaths)+len(oldPaths))
	seen := make(map[string]struct{})

	// Helper to append unique paths
	add := func(paths []string) {
		for _, p := range paths {
			if p == "" {
				continue
			}
			if _, exists := seen[p]; !exists {
				finalPaths = append(finalPaths, p)
				seen[p] = struct{}{}
			}
		}
	}
	// high priority paths go first
	add(newPaths)
	// then low priority paths
	add(oldPaths)

	return strings.Join(finalPaths, sep)
}

func Ordinal(i int) string {
	if i == 0 {
		return "first"
	}
	n := i

	// Logic for st, nd, rd, th
	suffix := "th"
	switch n % 100 {
	case 11, 12, 13:
		suffix = "th"
	default:
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}

	return fmt.Sprintf("%d%s", n, suffix)
}
