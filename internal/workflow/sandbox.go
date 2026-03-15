package workflow

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

// The sandbox ensures no arbitrary code execution (CVE-2026-25253 mitigation).
// All dynamic inputs in a workflow are string-templated, not `os/exec` or raw JS evaluation.

var envVarRegex = regexp.MustCompile(`\${[A-Z_]+}`)

// EvaluateMap processes a map of potentially templated strings using the context.
func EvaluateMap(input map[string]interface{}, ctx *ExecutionContext) (map[string]interface{}, error) {
	if input == nil {
		return nil, nil
	}

	output := make(map[string]interface{})
	for k, v := range input {
		switch value := v.(type) {
		case string:
			res, err := EvaluateString(value, ctx)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", k, err)
			}
			output[k] = res
		case map[string]interface{}:
			res, err := EvaluateMap(value, ctx)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", k, err)
			}
			output[k] = res
		case []interface{}:
			var arr []interface{}
			for i, item := range value {
				if str, ok := item.(string); ok {
					res, err := EvaluateString(str, ctx)
					if err != nil {
						return nil, fmt.Errorf("key %q[%d]: %w", k, i, err)
					}
					arr = append(arr, res)
				} else {
					arr = append(arr, item)
				}
			}
			output[k] = arr
		default:
			output[k] = v // Pass through numbers, booleans directly
		}
	}
	return output, nil
}

// EvaluateString safe-evaluates Go templates using workflow context state.
// It explicitly denies access to os.Getenv or function execution outside pure text formatting.
func EvaluateString(tpl string, ctx *ExecutionContext) (string, error) {
	// Reject anything that looks like an attempt to read environment variables
	if envVarRegex.MatchString(tpl) {
		return "", fmt.Errorf("environment variable expansion is forbidden in workflow definitions")
	}

	t, err := template.New("sandbox").Option("missingkey=error").Funcs(sandboxFuncMap()).Parse(tpl)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	data := map[string]interface{}{
		"vars":  ctx.Vars,
		"steps": ctx.StepState,
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}

// sandboxFuncMap returns a restricted set of text-manipulation functions.
func sandboxFuncMap() template.FuncMap {
	return template.FuncMap{
		"trim":    strings.TrimSpace,
		"upper":   strings.ToUpper,
		"lower":   strings.ToLower,
		"default": func(def, val interface{}) interface{} {
			if str, ok := val.(string); ok && str == "" {
				return def
			}
			if val == nil {
				return def
			}
			return val
		},
	}
}

// Built-in handlers for deterministic operations (non-LLM)

func handleTransform(inputs map[string]interface{}) (interface{}, error) {
	tmpl, ok := inputs["template"]
	if !ok {
		return nil, fmt.Errorf("transform step requires 'template' input")
	}

	str, ok := tmpl.(string)
	if !ok {
		return nil, fmt.Errorf("'template' must be a string")
	}

	return str, nil
}

func handleCondition(inputs map[string]interface{}) (interface{}, error) {
	op, ok := inputs["operator"]
	if !ok {
		return nil, fmt.Errorf("condition step requires 'operator' input")
	}

	val1 := inputs["value1"]
	val2 := inputs["value2"]

	operatorStr := fmt.Sprintf("%v", op)
	v1Str := fmt.Sprintf("%v", val1)
	v2Str := fmt.Sprintf("%v", val2)

	var match bool
	switch operatorStr {
	case "==":
		match = v1Str == v2Str
	case "!=":
		match = v1Str != v2Str
	case "contains":
		match = strings.Contains(v1Str, v2Str)
	default:
		return nil, fmt.Errorf("unknown operator %q", operatorStr)
	}

	return map[string]interface{}{"result": match}, nil
}
