package routes

import (
	"fmt"
	"strings"
)

// Input declares the accepted query/header parameters for one operation.
// An empty declaration is intentional; nil means the contract is missing.
// JSON bodies use Operation.Request; GET/HEAD and bodyless actions have none.
type Input struct {
	Parameters  []Parameter
	Description string
}

type Parameter struct {
	Name, In, Type, Format, Description string
	Required                            bool
	MaxBytes                            int
	Minimum, Maximum                    *int64
	Enum                                []string
}

func Inputs(parameters ...Parameter) *Input { return &Input{Parameters: parameters} }

func (input *Input) Describe(description string) *Input {
	input.Description = description
	return input
}

func Query(name string, required bool, maxBytes int) Parameter {
	return Parameter{Name: name, In: "query", Type: "string", Required: required, MaxBytes: maxBytes}
}

func Header(name string, required bool) Parameter {
	return Parameter{Name: name, In: "header", Type: "string", Required: required}
}

func IntegerQuery(name string, required bool, minimum, maximum int64) Parameter {
	return Parameter{Name: name, In: "query", Type: "integer", Format: "int64", Required: required, Minimum: &minimum, Maximum: &maximum}
}

func EnumQuery(name string, required bool, values ...string) Parameter {
	return Parameter{Name: name, In: "query", Type: "string", Required: required, Enum: values}
}

func (p Parameter) Describe(description string) Parameter { p.Description = description; return p }

func (p Parameter) AsDate() Parameter { p.Format = "date"; return p }

func (p Parameter) AsInteger(minimum, maximum int64) Parameter {
	p.Type, p.Format, p.Minimum, p.Maximum = "integer", "int64", &minimum, &maximum
	return p
}

func cloneInput(input *Input) *Input {
	if input == nil {
		return nil
	}
	copy := Inputs(append([]Parameter(nil), input.Parameters...)...)
	copy.Description = input.Description
	for i := range copy.Parameters {
		p := &copy.Parameters[i]
		p.Enum = append([]string(nil), p.Enum...)
		if p.Minimum != nil {
			value := *p.Minimum
			p.Minimum = &value
		}
		if p.Maximum != nil {
			value := *p.Maximum
			p.Maximum = &value
		}
	}
	return copy
}

func validateInput(op Operation) error {
	if op.Input == nil {
		return fmt.Errorf("missing explicit input contract")
	}
	if op.Request != nil && op.BinaryRequest {
		return fmt.Errorf("conflicting request representations")
	}
	if (op.Method == "GET" || op.Method == "HEAD") && (op.Request != nil || op.BinaryRequest) {
		return fmt.Errorf("request body on read operation")
	}
	if len(op.Required) > 0 && op.Request == nil {
		return fmt.Errorf("required fields without JSON body")
	}
	seen := map[string]bool{}
	for _, p := range op.Input.Parameters {
		if p.Name == "" || strings.ContainsAny(p.Name, " \t\r\n") || p.In != "query" && p.In != "header" {
			return fmt.Errorf("invalid parameter location/name")
		}
		name := p.Name
		if p.In == "header" {
			name = strings.ToLower(name)
		}
		key := p.In + "/" + name
		if seen[key] {
			return fmt.Errorf("duplicate parameter: %s", p.Name)
		}
		seen[key] = true
		if p.Type != "string" && p.Type != "integer" && p.Type != "boolean" {
			return fmt.Errorf("invalid parameter type: %s", p.Name)
		}
		if p.MaxBytes < 0 || p.Minimum != nil && p.Maximum != nil && *p.Minimum > *p.Maximum {
			return fmt.Errorf("invalid parameter limits: %s", p.Name)
		}
		if p.Type != "string" && (p.MaxBytes != 0 || len(p.Enum) != 0) || p.Type != "integer" && (p.Minimum != nil || p.Maximum != nil) {
			return fmt.Errorf("incompatible parameter limits: %s", p.Name)
		}
	}
	return nil
}
