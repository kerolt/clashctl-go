package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Value reads one scalar value through a sequence of mapping keys.
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

// Values reads scalar values from a YAML sequence selected by mapping keys.
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

// Bool reads and parses a boolean configuration value.
func Bool(path string, keys ...string) (bool, error) {
	value, err := Value(path, keys...)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(value)
}

// SetString updates one top-level string configuration value.
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

// SetBool updates one top-level boolean configuration value.
func SetBool(path, key string, value bool) error {
	return SetBoolPath(path, []string{key}, value)
}

// SetBoolPath updates a boolean value selected by nested mapping keys.
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

// SetInt updates an integer value selected by nested mapping keys.
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

// Read reads one YAML document root node from path.
func Read(path string) (*yaml.Node, error) {
	return readNode(path)
}

// Write writes one YAML document root node to path.
func Write(path string, node *yaml.Node) error {
	return writeNode(path, node)
}

// Copy copies a configuration file to destination.
func Copy(path, destination string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o644)
}
