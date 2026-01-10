package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/rhysd/actionlint"
)

type InputsProxy struct {
	ctx *ExecutionState
}

type GhContextProxy struct {
	GhContext map[string]any
}

type GhNeedsProxy struct {
	GhNeeds map[string]any
}

type GhMatrixProxy struct {
	GhMatrix map[string]any
}

type EnvProxy struct {
	Env map[string]string
}

type SecretsProxy struct {
	Secrets map[string]string
}

type Evaluator struct {
	ctx *ExecutionState
}

func NewEvaluator(ctx *ExecutionState) *Evaluator {
	return &Evaluator{ctx: ctx}
}

func (i *InputsProxy) GetInput(key string) (any, error) {
	if val, ok := i.ctx.Inputs[key]; ok {
		return val, nil
	}

	if len(i.ctx.Visited) == 0 {
		return nil, nil
	}

	currentVisit := i.ctx.Visited[len(i.ctx.Visited)-1]
	parent := currentVisit.Node.GetParent()
	if parent != nil {

		if groupNode, ok := parent.(NodeWithInputs); ok {
			v, err := groupNode.InputValueById(i.ctx, groupNode, InputId(key), nil)
			return v, err
		}
	} /* else {
		// in the root level, `ctx.Inputs` above should
		// have already returned the value, or the value doesn't exist
	}
	*/

	return nil, nil
}

func rewriteEnvToDotProperty(input string) string {
	l := actionlint.NewExprLexer(input)
	var tokens []*actionlint.Token

	for {
		t := l.Next()
		if t.Kind == actionlint.TokenKindEnd {
			break
		}
		tokens = append(tokens, t)
	}

	var sb strings.Builder
	lastOffset := 0

	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if t.Kind == actionlint.TokenKindDot && i+1 < len(tokens) {
			if i > 0 && strings.EqualFold(tokens[i-1].Value, "env") {
				nextT := tokens[i+1]
				if nextT.Kind == actionlint.TokenKindIdent {
					sb.WriteString(input[lastOffset:t.Offset])
					sb.WriteString("['")
					sb.WriteString(nextT.Value)
					sb.WriteString("']")
					lastOffset = nextT.Offset + len(nextT.Value)
					i++
					continue
				}
			}
		}
	}

	if lastOffset < len(input) {
		sb.WriteString(input[lastOffset:])
	}

	return sb.String()
}

func (e *Evaluator) Evaluate(input string) (any, error) {
	if !strings.Contains(input, "${{") {
		return input, nil
	}

	var parts []any
	lastIdx := 0

	for lastIdx < len(input) {
		start := strings.Index(input[lastIdx:], "${{")
		if start == -1 {
			parts = append(parts, input[lastIdx:])
			break
		}
		start += lastIdx
		if start > lastIdx {
			parts = append(parts, input[lastIdx:start])
		}

		relEnd := strings.Index(input[start+3:], "}}")
		if relEnd == -1 {
			return nil, fmt.Errorf("unclosed expression starting at %d", start)
		}
		exprEnd := start + 3 + relEnd + 2

		candidateExpr := input[start:exprEnd]
		val, err := e.parseExpressionString(candidateExpr)
		if err != nil {
			if !errors.Is(err, &ErrNoInputValue{}) && !errors.Is(err, &ErrNoOutputValue{}) {
				return nil, fmt.Errorf("failed to parse expression at %d: %w", start, err)
			}
		}
		parts = append(parts, val)
		lastIdx = exprEnd
	}

	if len(parts) == 1 && lastIdx == len(input) && strings.HasPrefix(input, "${{") {
		return parts[0], nil
	}

	var sb strings.Builder
	for _, part := range parts {
		if part == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("%v", part))
	}
	return sb.String(), nil
}

func (e *Evaluator) parseExpressionString(input string) (any, error) {
	if !strings.HasPrefix(input, "${{") || !strings.HasSuffix(input, "}}") {
		return nil, fmt.Errorf("invalid expression format")
	}
	inner := input[3:]

	// The actionlint parser is case-insensitive, turning `env.FOO` into `env.foo`. There are a few reported
	// issues and I'm not sure if there is a better workaround, but I saw that this doesn't happen for
	// env['FOO'] syntax which is idential to env.FOO, so this function rewrites all dot property accessors
	// into bracket accesses to preserve the casing. The line responsible for this toLower is here:
	// https://github.com/rhysd/actionlint/blob/ff3994b5657e8001ba8f0a06d2bd7a76e3c3d684/expr_parser.go#L218
	inner = rewriteEnvToDotProperty(inner)
	parser := actionlint.NewExprParser()
	exprNode, err := parser.Parse(actionlint.NewExprLexer(inner))
	if err != nil {
		return nil, err
	}
	return e.evalNode(exprNode)
}

func (e *Evaluator) callFunction(name string, args []any) (any, error) {
	switch strings.ToLower(name) {
	case "always":
		return true, nil
	case "success":
		return true, nil
	case "failure":
		return false, nil
	case "cancelled":
		return false, nil

	case "fromjson":
		if len(args) < 1 {
			return nil, fmt.Errorf("fromJSON requires 1 argument")
		}
		str, ok := args[0].(string)
		if !ok {
			return args[0], nil
		}
		var result any
		if err := json.Unmarshal([]byte(str), &result); err != nil {
			return nil, err
		}
		return result, nil

	case "tojson":
		if len(args) < 1 {
			return nil, fmt.Errorf("toJSON requires 1 argument")
		}
		b, err := json.MarshalIndent(args[0], "", "  ")
		return string(b), err

	case "contains":
		if len(args) < 2 {
			return false, nil
		}
		searchItem := args[1]
		container := args[0]
		val := reflect.ValueOf(container)

		if val.Kind() == reflect.String {
			return strings.Contains(val.String(), fmt.Sprintf("%v", searchItem)), nil
		}
		if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
			for i := 0; i < val.Len(); i++ {
				if compareEquality(val.Index(i).Interface(), searchItem) {
					return true, nil
				}
			}
			return false, nil
		}
		return false, nil

	case "startswith":
		if len(args) < 2 {
			return false, nil
		}
		s, _ := args[0].(string)
		prefix, _ := args[1].(string)
		return strings.HasPrefix(s, prefix), nil

	case "endswith":
		if len(args) < 2 {
			return false, nil
		}
		s, _ := args[0].(string)
		suffix, _ := args[1].(string)
		return strings.HasSuffix(s, suffix), nil

	case "format":
		if len(args) < 1 {
			return "", nil
		}
		formatStr, _ := args[0].(string)
		for i, arg := range args[1:] {
			placeholder := fmt.Sprintf("{%d}", i)
			valStr := fmt.Sprintf("%v", arg)
			formatStr = strings.ReplaceAll(formatStr, placeholder, valStr)
		}
		return formatStr, nil

	case "join":
		if len(args) < 2 {
			return "", nil
		}
		val := reflect.ValueOf(args[0])
		sep, _ := args[1].(string)
		if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
			return "", nil
		}
		strs := make([]string, val.Len())
		for i := 0; i < val.Len(); i++ {
			strs[i] = fmt.Sprintf("%v", val.Index(i).Interface())
		}
		return strings.Join(strs, sep), nil

	case "hashfiles":
		if len(args) < 1 {
			return "", nil
		}
		patterns := make([]string, 0, len(args))
		for _, arg := range args {
			if pattern, ok := arg.(string); ok {
				patterns = append(patterns, pattern)
			}
		}
		return e.hashFiles(patterns...)
	}
	return nil, fmt.Errorf("unknown function: %s", name)
}

func (e *Evaluator) hashFiles(patterns ...string) (string, error) {
	if len(patterns) == 0 {
		return "", nil
	}

	var matchedFiles []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue // no idea atm if the original GH behavior also skips on invalid patterns
		}
		matchedFiles = append(matchedFiles, matches...)
	}

	fileSet := make(map[string]bool)
	for _, f := range matchedFiles {
		fileSet[f] = true
	}
	var uniqueFiles []string
	for f := range fileSet {
		info, err := os.Stat(f)
		if err == nil && info.Mode().IsRegular() {
			uniqueFiles = append(uniqueFiles, f)
		}
	}
	sort.Strings(uniqueFiles)

	if len(uniqueFiles) == 0 {
		return "", nil
	}

	hasher := sha256.New()
	for _, filePath := range uniqueFiles {
		file, err := os.Open(filePath)
		if err != nil {
			continue
		}
		if _, err := io.Copy(hasher, file); err != nil {
			file.Close()
			continue
		}
		file.Close()
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (e *Evaluator) resolveRootVar(name string) (any, error) {
	switch strings.ToLower(name) {
	case "env":
		return &EnvProxy{Env: e.ctx.Env}, nil
	case "secrets":
		return &SecretsProxy{Secrets: e.ctx.Secrets}, nil
	case "github":
		return &GhContextProxy{GhContext: e.ctx.GhContext}, nil
	case "needs":
		return &GhNeedsProxy{GhNeeds: e.ctx.GhNeeds}, nil
	case "steps":
		return e.ctx.DataOutputCache, nil
	case "inputs":
		return &InputsProxy{ctx: e.ctx}, nil
	case "matrix":
		return &GhMatrixProxy{GhMatrix: e.ctx.GhMatrix}, nil
	case "runner":
		return getRunnerInfo(e.ctx.Env), nil
	case "job":
		// TODO: (Seb) add the job context info in ExecutionState and call here
		return nil, errors.New("access to 'job' variable is not supported in this context")
	}
	return nil, nil
}

func (e *Evaluator) evalNode(node actionlint.ExprNode) (any, error) {
	switch n := node.(type) {
	case *actionlint.IntNode:
		return n.Value, nil
	case *actionlint.FloatNode:
		return n.Value, nil
	case *actionlint.StringNode:
		return n.Value, nil
	case *actionlint.BoolNode:
		return n.Value, nil
	case *actionlint.NullNode:
		return nil, nil

	case *actionlint.VariableNode:
		return e.resolveRootVar(n.Name)

	case *actionlint.ObjectDerefNode:
		return e.evaluateObjectDeref(n)

	case *actionlint.ArrayDerefNode:
		return e.evaluateArrayDeref(n)

	case *actionlint.IndexAccessNode:
		operand, err := e.evalNode(n.Operand)
		if err != nil {
			return nil, err
		}
		index, err := e.evalNode(n.Index)
		if err != nil {
			return nil, err
		}
		return e.resolveIndex(operand, index)

	case *actionlint.FuncCallNode:
		args := make([]any, len(n.Args))
		for i, arg := range n.Args {
			val, err := e.evalNode(arg)
			if err != nil {
				return nil, err
			}
			args[i] = val
		}
		return e.callFunction(n.Callee, args)

	case *actionlint.LogicalOpNode:
		left, err := e.evalNode(n.Left)
		if err != nil {
			return nil, err
		}
		switch n.Kind {
		case actionlint.LogicalOpNodeKindAnd:
			if !isTruthy(left) {
				return left, nil
			}
			return e.evalNode(n.Right)
		case actionlint.LogicalOpNodeKindOr:
			if isTruthy(left) {
				return left, nil
			}
			return e.evalNode(n.Right)
		default:
			return nil, fmt.Errorf("unknown logical op kind: %v", n.Kind)
		}

	case *actionlint.NotOpNode:
		val, err := e.evalNode(n.Operand)
		if err != nil {
			return nil, err
		}
		return !isTruthy(val), nil

	case *actionlint.CompareOpNode:
		left, err := e.evalNode(n.Left)
		if err != nil {
			return nil, err
		}
		right, err := e.evalNode(n.Right)
		if err != nil {
			return nil, err
		}
		return compare(n.Kind, left, right)
	}

	return nil, fmt.Errorf("unsupported node type: %T", node)
}

func (e *Evaluator) evaluateArrayDeref(node *actionlint.ArrayDerefNode) (any, error) {
	receiverVal, err := e.evalNode(node.Receiver)
	if err != nil {
		return nil, err
	}

	switch v := receiverVal.(type) {
	case *GhNeedsProxy:
		receiverVal = v.GhNeeds
	case *GhContextProxy:
		receiverVal = v.GhContext
	case *GhMatrixProxy:
		receiverVal = v.GhMatrix
	case *EnvProxy:
		receiverVal = v.Env
	case *SecretsProxy:
		receiverVal = v.Secrets
	case *InputsProxy:
		receiverVal = v.ctx.Inputs
	}

	switch m := receiverVal.(type) {
	case map[string]any:
		return toSortedValues(m), nil
	case map[string]string:
		return toSortedValues(m), nil
	default:
		return []any{}, nil
	}
}

func (e *Evaluator) evaluateObjectDeref(node *actionlint.ObjectDerefNode) (any, error) {
	receiverVal, err := e.evalNode(node.Receiver)
	if err != nil {
		return nil, err
	}

	propName := node.Property

	switch v := receiverVal.(type) {
	case *GhContextProxy:
		receiverVal = v.GhContext
	case *GhNeedsProxy:
		receiverVal = v.GhNeeds
	case *GhMatrixProxy:
		receiverVal = v.GhMatrix
	}

	switch v := receiverVal.(type) {
	case *InputsProxy:
		return v.GetInput(propName)

	case *SecretsProxy:
		key := strings.ToUpper(propName)
		if val, ok := v.Secrets[key]; ok {
			return val, nil
		}

	case map[string]any:
		return lookupCaseInsensitive(v, propName), nil

	case map[string]string:
		// string map behavior, exact match only
		if val, ok := v[propName]; ok {
			return val, nil
		}

	case []any:
		return projectSlice(v, propName), nil
	}

	return nil, nil
}

// lookupCaseInsensitive checks if either upper or lowercase is in a map.
// This needs to be better specified which context variables go with upper/lower case.
func lookupCaseInsensitive(m map[string]any, key string) any {
	if val, ok := m[strings.ToUpper(key)]; ok {
		return val
	}
	if val, ok := m[strings.ToLower(key)]; ok {
		return val
	}
	return nil
}

// projectSlice handles the "Object Filter" syntax: array.*.property
func projectSlice(slice []any, propName string) []any {
	projected := make([]any, len(slice))

	for i, item := range slice {
		// If the item isn't a map, the result is nil (standard GHA behavior).
		if itemMap, ok := item.(map[string]any); ok {
			// array projection usually implies exact match in JSON,
			// but if we need case-insensitivity here too, we could use lookupCaseInsensitive
			if val, exists := itemMap[propName]; exists {
				projected[i] = val
			} else {
				projected[i] = nil
			}
		} else {
			projected[i] = nil
		}
	}
	return projected
}

func (e *Evaluator) resolveIndex(obj any, index any) (any, error) {
	if obj == nil {
		return nil, nil
	}

	if proxy, ok := obj.(*EnvProxy); ok {
		key := fmt.Sprintf("%v", index)
		if val, ok := proxy.Env[key]; ok {
			return val, nil
		}
		return nil, nil
	}

	val := reflect.ValueOf(obj)

	if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
		// integer index
		if idx, ok := toInt(index); ok {
			if idx < 0 || idx >= val.Len() {
				return nil, nil
			}
			return val.Index(idx).Interface(), nil
		}
		// string index (array projection via index syntax)
		if key, ok := index.(string); ok {
			length := val.Len()
			projection := make([]any, length)
			for i := range length {
				item := val.Index(i).Interface()
				res, _ := e.resolveIndex(item, key)
				projection[i] = res
			}
			return projection, nil
		}
		return nil, fmt.Errorf("array index must be integer or string projection (got %T)", index)
	}

	if val.Kind() == reflect.Map {
		keyStr := fmt.Sprintf("%v", index)
		keyVal := reflect.ValueOf(keyStr)
		res := val.MapIndex(keyVal)
		if res.IsValid() {
			return res.Interface(), nil
		}
		iter := val.MapRange()
		for iter.Next() {
			k := iter.Key()
			if strings.EqualFold(fmt.Sprintf("%v", k.Interface()), keyStr) {
				return iter.Value().Interface(), nil
			}
		}
		return nil, nil
	}
	return nil, fmt.Errorf("cannot index type %T", obj)
}

func isTruthy(val any) bool {
	// https://docs.github.com/en/actions/reference/workflows-and-actions/expressions#literals
	// From the docs: Note that in conditionals, falsy values (false, 0, -0, "", '', null) are
	// coerced to false and truthy (true and other non-falsy values) are coerced to true.
	if val == nil {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case float64:
		return v != 0 && !math.IsNaN(v)
	case int64:
		return v != 0
	case int32:
		return v != 0
	default:
		// everything else is always truthy
		return true
	}
}

func toInt(i any) (int, bool) {
	switch v := i.(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case string:
		i, err := strconv.Atoi(v)
		if err == nil {
			return i, true
		}
		// also handle float-looking strings "0.0" -> 0
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return int(f), true
		}
		return 0, false
	default:
		return 0, false
	}
}

func toNumber(val any) (float64, bool) {
	if val == nil {
		return 0, true
	}
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case int16:
		return float64(v), true
	case int8:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case string:
		// GHA ignores whitespace when coercing string to number
		v = strings.TrimSpace(v)
		if v == "" {
			return 0, true
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return math.NaN(), false // Not a number
		}
		return f, true
	default:
		return math.NaN(), false
	}
}

func compare(kind actionlint.CompareOpNodeKind, left, right any) (bool, error) {
	switch kind {
	case actionlint.CompareOpNodeKindEq:
		return compareEquality(left, right), nil
	case actionlint.CompareOpNodeKindNotEq:
		return !compareEquality(left, right), nil
	default:
		return compareRelational(kind, left, right)
	}
}

// compareEquality handles == and != with type coercion.
// strategy: number -> string (case-insensitive) -> direct
func compareEquality(left, right any) bool {
	// first if both are null, they are equal
	if left == nil && right == nil {
		return true
	}

	// if both are strings, use case-insensitive comparison
	lStr, lIsStr := left.(string)
	rStr, rIsStr := right.(string)
	if lIsStr && rIsStr {
		return strings.EqualFold(lStr, rStr)
	}

	// attempt for numeric coercion
	// In GHA, if types differ (e.g. bool vs string, null vs number),
	// everything tries to become a number. I dont know why that is.
	lNum, lIsNum := toNumber(left)
	rNum, rIsNum := toNumber(right)

	// If coercion was possible for both (even if one became NaN)
	// toNumber returns false only for objects/arrays.
	// Strings that aren't numbers return true but with value NaN.
	if lIsNum && rIsNum {
		// NaN != NaN
		if math.IsNaN(lNum) || math.IsNaN(rNum) {
			return false
		}
		return lNum == rNum
	}

	// fallback for objects and arrays
	return reflect.DeepEqual(left, right)
}

func compareRelational(kind actionlint.CompareOpNodeKind, left, right any) (bool, error) {
	// First if both are strings, use lexicographical comparison here (case-insensitive)
	lStr, lIsStr := left.(string)
	rStr, rIsStr := right.(string)

	if lIsStr && rIsStr {
		lLower := strings.ToLower(lStr)
		rLower := strings.ToLower(rStr)
		switch kind {
		case actionlint.CompareOpNodeKindLess:
			return lLower < rLower, nil
		case actionlint.CompareOpNodeKindLessEq:
			return lLower <= rLower, nil
		case actionlint.CompareOpNodeKindGreater:
			return lLower > rLower, nil
		case actionlint.CompareOpNodeKindGreaterEq:
			return lLower >= rLower, nil
		}
	}

	// numeric comparison. null coerces to 0 here too.
	lNum, lOk := toNumber(left)
	rNum, rOk := toNumber(right)

	if !lOk || !rOk || math.IsNaN(lNum) || math.IsNaN(rNum) {
		return false, nil
	}

	switch kind {
	case actionlint.CompareOpNodeKindLess:
		return lNum < rNum, nil
	case actionlint.CompareOpNodeKindLessEq:
		return lNum <= rNum, nil
	case actionlint.CompareOpNodeKindGreater:
		return lNum > rNum, nil
	case actionlint.CompareOpNodeKindGreaterEq:
		return lNum >= rNum, nil
	}

	return false, fmt.Errorf("unknown relational operator")
}

func getRunnerInfo(env map[string]string) map[string]any {

	var osName string
	switch runtime.GOOS {
	case "windows":
		osName = "Windows"
	case "darwin":
		osName = "macOS"
	default:
		osName = runtime.GOOS
	}

	var archName string
	switch runtime.GOARCH {
	case "amd64":
		archName = "X64"
	case "386":
		archName = "X86"
	case "arm64":
		archName = "ARM64"
	case "arm":
		archName = "ARM"
	default:
		archName = runtime.GOARCH
	}

	return map[string]any{
		"os":         osName,
		"arch":       archName,
		"name":       env["RUNNER_NAME"],
		"temp":       env["RUNNER_TEMP"],
		"tool_cache": env["RUNNER_TOOL_CACHE"],
	}
}

func toSortedValues[V any](m map[string]V) []any {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	values := make([]any, 0, len(m))
	for _, k := range keys {
		values = append(values, any(m[k])) // convert to any here
	}
	return values
}
