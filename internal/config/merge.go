package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

// BuildRuntime 在内存中把 Mixin 规则应用到基础配置。
func BuildRuntime(base, mixin *yaml.Node) (*yaml.Node, error) {
	// 复制输入节点，避免修改调用方持有的 YAML 树。
	base = cloneNode(base)
	mixin = cloneNode(mixin)
	if base.Kind != yaml.MappingNode || mixin.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("base and mixin configurations must be mappings")
	}

	baseRules := cloneNode(mappingValue(base, "rules"))
	configProxies := cloneNode(mappingValue(base, "proxies"))
	configProxyGroups := cloneNode(mappingValue(base, "proxy-groups"))
	mixinRules := cloneNode(mappingValue(mixin, "rules"))
	mixinProxies := cloneNode(mappingValue(mixin, "proxies"))
	mixinProxyGroups := cloneNode(mappingValue(mixin, "proxy-groups"))

	deleteMappingKey(mixin, "_custom")

	// 先执行普通映射合并，再按 clashctl 约定处理特殊序列。
	mergeMapping(base, mixin)

	setMappingValue(base, "rules", concatSequences(
		mappingValue(mixinRules, "prepend"),
		baseRules,
		mappingValue(mixinRules, "append"),
	))

	defaultProxyGroup := defaultGroupName(configProxyGroups)

	// 每条规则分别替换一次内部代理组占位符。
	if rules := mappingValue(base, "rules"); rules != nil && rules.Kind == yaml.SequenceNode {
		for _, rule := range rules.Content {
			replaceFirst(rule, "__CLASHCTL_PROXY_GROUP__", defaultProxyGroup)
		}
	}

	setMappingValue(base, "proxies", mergeNamedSequences(
		mappingValue(mixinProxies, "prepend"),
		configProxies,
		mappingValue(mixinProxies, "override"),
		mappingValue(mixinProxies, "append"),
	))
	setMappingValue(base, "proxy-groups", mergeNamedSequences(
		mappingValue(mixinProxyGroups, "prepend"),
		configProxyGroups,
		mappingValue(mixinProxyGroups, "override"),
		mappingValue(mixinProxyGroups, "append"),
	))

	injectProxyGroups(base, mappingValue(mixinProxyGroups, "inject"))
	return base, nil
}

// Merge 生成并校验运行配置，校验失败时恢复上一份文件。
func Merge(ctx context.Context, paths Paths, kernelPath string) error {
	// 读取并构建新的运行配置。
	base, err := readNode(paths.BaseConfig)
	if err != nil {
		return fmt.Errorf("read base configuration: %w", err)
	}
	mixin, err := readNode(paths.MixinConfig)
	if err != nil {
		return fmt.Errorf("read mixin configuration: %w", err)
	}
	runtime, err := BuildRuntime(base, mixin)
	if err != nil {
		return fmt.Errorf("build runtime configuration: %w", err)
	}

	previous, previousErr := os.ReadFile(paths.RuntimeConfig)
	if previousErr == nil {
		if err := os.WriteFile(paths.TemporaryConfig, previous, 0o644); err != nil {
			return fmt.Errorf("save previous runtime configuration: %w", err)
		}
	}
	if err := writeNode(paths.RuntimeConfig, runtime); err != nil {
		return fmt.Errorf("write runtime configuration: %w", err)
	}

	// 内核校验失败时恢复旧运行配置。
	if err := Validate(ctx, kernelPath, paths.RuntimeConfig); err != nil {
		if previousErr == nil {
			if restoreErr := os.WriteFile(paths.RuntimeConfig, previous, 0o644); restoreErr != nil {
				return fmt.Errorf("validate runtime configuration: %v; restore runtime configuration: %w", err, restoreErr)
			}
		}
		return fmt.Errorf("validate runtime configuration: %w", err)
	}
	return nil
}

// Validate 调用内核校验一个配置文件。
func Validate(ctx context.Context, kernelPath, configPath string) error {
	command := exec.CommandContext(
		ctx,
		kernelPath,
		"-d",
		directory(configPath),
		"-f",
		configPath,
		"-t",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, message)
	}
	return nil
}

// readNode 读取一个 YAML 文档并返回根节点。
func readNode(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if len(document.Content) != 1 {
		return nil, fmt.Errorf("configuration document is empty")
	}
	return document.Content[0], nil
}

// writeNode 把一个 YAML 根节点编码后写入文件。
func writeNode(path string, node *yaml.Node) error {
	var buffer bytes.Buffer
	document := yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{node}}
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buffer.Bytes(), 0o644)
}

// cloneNode 完整复制一棵 YAML 节点树。
func cloneNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	copy := *node
	copy.Content = make([]*yaml.Node, len(node.Content))
	for index, child := range node.Content {
		copy.Content[index] = cloneNode(child)
	}
	return &copy
}

// mappingValue 从 YAML 映射中取出指定键的值。
func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

// setMappingValue 插入或覆盖一个 YAML 映射值。
func setMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content[index+1] = value
			return
		}
	}
	mapping.Content = append(
		mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

// deleteMappingKey 从 YAML 映射中删除一个键。
func deleteMappingKey(mapping *yaml.Node, key string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	content := make([]*yaml.Node, 0, len(mapping.Content))
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			continue
		}
		content = append(content, mapping.Content[index], mapping.Content[index+1])
	}
	mapping.Content = content
}

// mergeMapping 递归合并映射，标量与序列直接覆盖。
func mergeMapping(destination, source *yaml.Node) {
	for index := 0; index+1 < len(source.Content); index += 2 {
		key := source.Content[index].Value
		sourceValue := source.Content[index+1]
		destinationValue := mappingValue(destination, key)
		if destinationValue != nil && destinationValue.Kind == yaml.MappingNode && sourceValue.Kind == yaml.MappingNode {
			mergeMapping(destinationValue, sourceValue)
			continue
		}
		setMappingValue(destination, key, cloneNode(sourceValue))
	}
}

// concatSequences 拼接 YAML 序列，并复制其中的元素。
func concatSequences(sequences ...*yaml.Node) *yaml.Node {
	result := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, sequence := range sequences {
		if sequence == nil || sequence.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range sequence.Content {
			result.Content = append(result.Content, cloneNode(item))
		}
	}
	return result
}

// mergeNamedSequences 在 prepend 与 append 之间按名称应用覆盖。
func mergeNamedSequences(prepend, base, overrides, appendItems *yaml.Node) *yaml.Node {
	result := concatSequences(prepend)
	if base != nil && base.Kind == yaml.SequenceNode {
		for _, item := range base.Content {
			result.Content = append(result.Content, cloneNode(findOverride(item, overrides)))
		}
	}
	result = concatSequences(result, appendItems)
	return result
}

// findOverride 按名称查找序列元素对应的替换项。
func findOverride(item, overrides *yaml.Node) *yaml.Node {
	name := scalarValue(mappingValue(item, "name"))
	if name == "" || overrides == nil || overrides.Kind != yaml.SequenceNode {
		return item
	}
	for _, override := range overrides.Content {
		if scalarValue(mappingValue(override, "name")) == name {
			return override
		}
	}
	return item
}

// defaultGroupName 选择用于替换规则占位符的默认代理组。
func defaultGroupName(groups *yaml.Node) string {
	if groups == nil || groups.Kind != yaml.SequenceNode {
		return ""
	}
	var firstSelect string
	for _, group := range groups.Content {
		if scalarValue(mappingValue(group, "type")) != "select" {
			continue
		}
		name := scalarValue(mappingValue(group, "name"))
		if firstSelect == "" {
			firstSelect = name
		}
		if strings.Contains(name, "节点选择") {
			return name
		}
	}
	return firstSelect
}

// replaceFirst 在 YAML 节点树中替换第一处匹配的字符串。
func replaceFirst(node *yaml.Node, old, new string) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" && strings.Contains(node.Value, old) {
		node.Value = strings.Replace(node.Value, old, new, 1)
		return true
	}
	for _, child := range node.Content {
		if replaceFirst(child, old, new) {
			return true
		}
	}
	return false
}

// injectProxyGroups 向匹配的代理组追加节点，并去除重复项。
func injectProxyGroups(runtime, inject *yaml.Node) {
	groups := mappingValue(runtime, "proxy-groups")
	if groups == nil || groups.Kind != yaml.SequenceNode || inject == nil || inject.Kind != yaml.MappingNode {
		return
	}
	for _, group := range groups.Content {
		name := scalarValue(mappingValue(group, "name"))
		extra := mappingValue(inject, name)
		if name == "" || extra == nil || extra.Kind != yaml.SequenceNode {
			continue
		}
		proxies := mappingValue(group, "proxies")
		setMappingValue(group, "proxies", uniqueSequence(concatSequences(proxies, extra)))
	}
}

// uniqueSequence 按顺序去重 YAML 序列。
func uniqueSequence(sequence *yaml.Node) *yaml.Node {
	result := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	seen := make(map[string]struct{})
	for _, item := range sequence.Content {
		key := nodeKey(item)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result.Content = append(result.Content, cloneNode(item))
	}
	return result
}

// nodeKey 生成单个 YAML 节点的序列化标识。
func nodeKey(node *yaml.Node) string {
	var buffer bytes.Buffer
	_ = yaml.NewEncoder(&buffer).Encode(node)
	return buffer.String()
}

// scalarValue 返回标量 YAML 节点的值。
func scalarValue(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

// directory 返回路径的父目录部分。
func directory(path string) string {
	index := strings.LastIndexByte(path, os.PathSeparator)
	if index < 0 {
		return "."
	}
	return path[:index]
}
