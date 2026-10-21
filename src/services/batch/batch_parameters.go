package batch

import (
	"strings"

	"go.yaml.in/yaml/v3"

	"minio-web/api/src/services/apperror"
)

// This is a presentation DTO, never the source job document. In particular,
// replication credentials, notification tokens and KMS context stay on MinIO.
type BatchParameters struct {
	Type       string          `json:"type"`
	Supported  bool            `json:"supported"`
	Bucket     string          `json:"bucket,omitempty"`
	Prefixes   []string        `json:"prefixes,omitempty"`
	Rules      []BatchRuleView `json:"rules,omitempty"`
	Encryption string          `json:"encryption,omitempty"`
	KeyID      string          `json:"keyId,omitempty"`
	Partial    bool            `json:"partial"`
}
type BatchRuleView struct {
	Type           string `json:"type,omitempty"`
	Name           string `json:"name,omitempty"`
	OlderThan      string `json:"olderThan,omitempty"`
	CreatedBefore  string `json:"createdBefore,omitempty"`
	RetainVersions *int   `json:"retainVersions,omitempty"`
}

func SafeBatchParameters(raw string) (BatchParameters, error) {
	invalid := func() (BatchParameters, error) {
		// YAML decoding errors may include the original source line with secrets.
		return BatchParameters{}, apperror.New(502, "InvalidBatchParameters", "MinIO вернул параметры задания в неподдерживаемом формате.")
	}
	if len(raw) > 1<<20 {
		return invalid()
	}
	var root yaml.Node
	if yaml.Unmarshal([]byte(raw), &root) != nil || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return invalid()
	}
	get := func(node *yaml.Node, key string) *yaml.Node {
		if node == nil || node.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == key {
				return node.Content[i+1]
			}
		}
		return nil
	}
	scalar := func(n *yaml.Node) string {
		if n != nil && n.Kind == yaml.ScalarNode && n.Tag != "!!null" {
			return n.Value
		}
		return ""
	}
	unknown := func(n *yaml.Node, allowed string) bool {
		if n == nil {
			return false
		}
		if n.Kind != yaml.MappingNode {
			return true
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			known := false
			for _, key := range strings.Split(allowed, "|") {
				if key == n.Content[i].Value {
					known = true
					break
				}
			}
			if !known {
				return true
			}
		}
		return false
	}
	var out BatchParameters
	var job *yaml.Node
	for _, kind := range []string{"expire", "keyrotate", "replicate", "catalog"} {
		if n := get(root.Content[0], kind); n != nil && n.Tag != "!!null" {
			if job != nil {
				return invalid()
			}
			out.Type, job = kind, n
		}
	}
	if job == nil || job.Kind != yaml.MappingNode {
		return invalid()
	}
	out.Supported = out.Type == "expire" || out.Type == "keyrotate"
	if !out.Supported {
		out.Partial = true
		return out, nil
	}
	out.Bucket = scalar(get(job, "bucket"))
	prefix := get(job, "prefix")
	if prefix == nil || prefix.Tag == "!!null" {
		out.Prefixes = []string{""}
	} else if prefix.Kind == yaml.ScalarNode {
		out.Prefixes = []string{prefix.Value}
	} else if prefix.Kind == yaml.SequenceNode {
		for _, n := range prefix.Content {
			if n.Kind != yaml.ScalarNode {
				return invalid()
			}
			out.Prefixes = append(out.Prefixes, n.Value)
		}
	} else {
		return invalid()
	}
	if len(out.Prefixes) == 0 {
		out.Prefixes = []string{""}
	}
	out.Partial = unknown(job, "apiVersion|bucket|prefix|rules|encryption") || scalar(get(job, "apiVersion")) != "v1"
	if out.Type == "expire" {
		rules := get(job, "rules")
		if rules == nil || rules.Kind != yaml.SequenceNode {
			return invalid()
		}
		for _, rule := range rules.Content {
			if rule.Kind != yaml.MappingNode {
				return invalid()
			}
			v := BatchRuleView{Type: scalar(get(rule, "type")), Name: scalar(get(rule, "name")), OlderThan: scalar(get(rule, "olderThan")), CreatedBefore: scalar(get(rule, "createdBefore"))}
			purge := get(rule, "purge")
			if retain := get(purge, "retainVersions"); retain != nil {
				var value int
				if retain.Decode(&value) != nil {
					return invalid()
				}
				v.RetainVersions = &value
			}
			out.Partial = out.Partial || unknown(rule, "type|name|olderThan|createdBefore|purge") || unknown(purge, "retainVersions")
			out.Rules = append(out.Rules, v)
		}
	} else {
		enc := get(job, "encryption")
		out.Encryption, out.KeyID = scalar(get(enc, "type")), scalar(get(enc, "key"))
		out.Partial = out.Partial || unknown(enc, "type|key")
	}
	return out, nil
}
