package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Value 按映射键逐层读取一个标量值。
func Value(path string, keys ...string) (string, error) {
	node, err := readNode(path)
	if err != nil {
		return "", fmt.Errorf("read configuration value: %w", err)
	}
	for _, key := range keys {
		node = mappingValue(node, key)
		if node == nil {
			return "", nil
		}
	}
	return scalarValue(node), nil
}

// Values 按映射键读取一个 YAML 序列中的全部标量值。
func Values(path string, keys ...string) ([]string, error) {
	node, err := readNode(path)
	if err != nil {
		return nil, fmt.Errorf("read configuration values: %w", err)
	}
	for _, key := range keys {
		node = mappingValue(node, key)
		if node == nil {
			return []string{}, nil
		}
	}
	if node.Kind != yaml.SequenceNode {
		return []string{}, nil
	}
	values := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		values = append(values, scalarValue(item))
	}
	return values, nil
}

// Bool 读取并解析布尔配置值。
func Bool(path string, keys ...string) (bool, error) {
	value, err := Value(path, keys...)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(value)
}

// SetString 更新一个顶层字符串配置值。
func SetString(path, key, value string) error {
	node, err := readNode(path)
	if err != nil {
		return fmt.Errorf("read configuration: %w", err)
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("configuration root is not a mapping")
	}
	setMappingValue(node, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
	return writeNode(path, node)
}

// SetBool 更新一个顶层布尔配置值。
func SetBool(path, key string, value bool) error {
	return SetBoolPath(path, []string{key}, value)
}

// SetBoolPath 按嵌套映射键更新布尔值。
func SetBoolPath(path string, keys []string, value bool) error {
	if len(keys) == 0 {
		return fmt.Errorf("configuration path is empty")
	}
	node, err := readNode(path)
	if err != nil {
		return fmt.Errorf("read configuration: %w", err)
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("configuration root is not a mapping")
	}
	for _, key := range keys[:len(keys)-1] {
		child := mappingValue(node, key)
		if child == nil {
			child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMappingValue(node, key, child)
		}
		node = child
		if node.Kind != yaml.MappingNode {
			return fmt.Errorf("configuration path %q is not a mapping", key)
		}
	}
	setMappingValue(node, keys[len(keys)-1], &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(value)})
	return writeNode(path, node)
}

// SetInt 按嵌套映射键更新整数值。
func SetInt(path string, keys []string, value int) error {
	if len(keys) == 0 {
		return fmt.Errorf("configuration path is empty")
	}
	node, err := readNode(path)
	if err != nil {
		return fmt.Errorf("read configuration: %w", err)
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("configuration root is not a mapping")
	}
	for _, key := range keys[:len(keys)-1] {
		child := mappingValue(node, key)
		if child == nil {
			child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMappingValue(node, key, child)
		}
		node = child
		if node.Kind != yaml.MappingNode {
			return fmt.Errorf("configuration path %q is not a mapping", key)
		}
	}
	setMappingValue(node, keys[len(keys)-1], &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(value)})
	return writeNode(path, node)
}

// Read 读取一个 YAML 文档根节点。
func Read(path string) (*yaml.Node, error) {
	return readNode(path)
}

// Write 把一个 YAML 文档根节点写入文件。
func Write(path string, node *yaml.Node) error {
	return writeNode(path, node)
}

// Copy 复制配置文件到目标路径。
func Copy(path, destination string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o644)
}
