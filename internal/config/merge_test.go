package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestBuildRuntime verifies the special Mixin merge rules.
func TestBuildRuntime(t *testing.T) {
	base := parseTestYAML(t, `
keep: base
rules:
  - DOMAIN,example.com,__CLASHCTL_PROXY_GROUP__
proxies:
  - name: A
    type: ss
  - name: B
    type: vmess
proxy-groups:
  - name: 节点选择
    type: select
    proxies: [A]
  - name: 自动选择
    type: url-test
    proxies: [A]
`)
	mixin := parseTestYAML(t, `
_custom:
  ignored: true
keep: mixin
rules:
  prepend:
    - DOMAIN-SUFFIX,internal.example,DIRECT
  append:
    - MATCH,__CLASHCTL_PROXY_GROUP__
proxies:
  prepend:
    - name: P
      type: direct
  override:
    - name: A
      type: trojan
  append:
    - name: C
      type: hysteria2
proxy-groups:
  prepend:
    - name: 前置组
      type: select
      proxies: [P]
  override:
    - name: 节点选择
      type: select
      proxies: [B]
  append:
    - name: 后置组
      type: select
      proxies: [C]
  inject:
    节点选择: [C, B, C]
`)

	runtime, err := BuildRuntime(base, mixin)
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	if got := scalarValue(mappingValue(runtime, "keep")); got != "mixin" {
		t.Fatalf("keep = %q, want %q", got, "mixin")
	}
	if mappingValue(runtime, "_custom") != nil {
		t.Fatal("runtime contains _custom")
	}

	rules := mappingValue(runtime, "rules")
	if got := rules.Content[0].Value; got != "DOMAIN-SUFFIX,internal.example,DIRECT" {
		t.Fatalf("first rule = %q", got)
	}
	if got := rules.Content[1].Value; got != "DOMAIN,example.com,节点选择" {
		t.Fatalf("base rule = %q", got)
	}
	if got := rules.Content[2].Value; got != "MATCH,节点选择" {
		t.Fatalf("last rule = %q", got)
	}

	if got := namedValues(mappingValue(runtime, "proxies")); got != "P,A,B,C" {
		t.Fatalf("proxies = %q, want %q", got, "P,A,B,C")
	}
	groups := mappingValue(runtime, "proxy-groups")
	if got := namedValues(groups); got != "前置组,节点选择,自动选择,后置组" {
		t.Fatalf("proxy groups = %q", got)
	}
	selected := groups.Content[3-2]
	if got := scalarValues(mappingValue(selected, "proxies")); got != "B,C" {
		t.Fatalf("injected proxies = %q, want %q", got, "B,C")
	}
}

// TestBuildRuntimeRejectsNonMappings verifies invalid YAML roots are rejected.
func TestBuildRuntimeRejectsNonMappings(t *testing.T) {
	_, err := BuildRuntime(parseTestYAML(t, "[]"), parseTestYAML(t, "{}"))
	if err == nil {
		t.Fatal("BuildRuntime() error = nil")
	}
}

// parseTestYAML parses one YAML fixture for merge tests.
func parseTestYAML(t *testing.T, value string) *yaml.Node {
	t.Helper()
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(value), &document); err != nil {
		t.Fatalf("parse yaml: %v", err)
	}
	return document.Content[0]
}

// scalarValues joins scalar sequence values for test assertions.
func scalarValues(sequence *yaml.Node) string {
	values := make([]string, 0, len(sequence.Content))
	for _, item := range sequence.Content {
		values = append(values, scalarValue(item))
	}
	return joinTestValues(values)
}

// namedValues joins name fields from mapping sequence items.
func namedValues(sequence *yaml.Node) string {
	values := make([]string, 0, len(sequence.Content))
	for _, item := range sequence.Content {
		values = append(values, scalarValue(mappingValue(item, "name")))
	}
	return joinTestValues(values)
}

// joinTestValues joins test values with commas.
func joinTestValues(values []string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += ","
		}
		result += value
	}
	return result
}
