package auth

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/Sunse666/NoneFrp/internal/config"
)

// ErrMissingConfig is returned when the server configuration does not exist yet.
var ErrMissingConfig = errors.New("server configuration not found")

// AppendTokenFile adds token to the token table of a server configuration file.
//
// The file is edited as a YAML document rather than as text, so both "tokens:"
// and "tokens: {}" are handled and comments survive.
func AppendTokenFile(path, token, note string) error {
	if !ValidToken(token) {
		return fmt.Errorf("invalid token %q", token)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrMissingConfig, path)
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	root := &document
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("%s does not contain a mapping at the top level", path)
	}

	tokens := findMappingValue(root, "tokens")
	if tokens == nil {
		tokens = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "tokens"},
			tokens,
		)
	}
	if tokens.Kind != yaml.MappingNode {
		return fmt.Errorf("tokens in %s is not a mapping", path)
	}

	for i := 0; i+1 < len(tokens.Content); i += 2 {
		if tokens.Content[i].Value == token {
			return fmt.Errorf("token %s is already present in %s", token, path)
		}
	}

	tokens.Content = append(tokens.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: token},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: note},
	)

	encoded, err := yaml.Marshal(&document)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// PutCredential adds or replaces a credential in the token table of path. A
// credential without a policy is written as a plain string.
func PutCredential(path, code string, credential config.Token) error {
	if code == "" {
		return fmt.Errorf("credential code must not be empty")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrMissingConfig, path)
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	root := &document
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("%s does not contain a mapping at the top level", path)
	}

	tokens := findMappingValue(root, "tokens")
	if tokens == nil {
		tokens = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "tokens"},
			tokens,
		)
	}
	if tokens.Kind != yaml.MappingNode {
		return fmt.Errorf("tokens in %s is not a mapping", path)
	}

	value, err := credentialNode(credential)
	if err != nil {
		return err
	}

	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: code}
	replaced := false
	for i := 0; i+1 < len(tokens.Content); i += 2 {
		if tokens.Content[i].Value == code {
			tokens.Content[i+1] = value
			replaced = true
			break
		}
	}
	if !replaced {
		tokens.Content = append(tokens.Content, key, value)
	}

	encoded, err := yaml.Marshal(&document)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// credentialNode renders a credential the way the loader expects to read it.
func credentialNode(credential config.Token) (*yaml.Node, error) {
	if credential.Permanent() {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: credential.Note}, nil
	}

	encoded, err := yaml.Marshal(credential)
	if err != nil {
		return nil, fmt.Errorf("encode credential: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(encoded, &document); err != nil {
		return nil, fmt.Errorf("decode credential: %w", err)
	}
	if len(document.Content) == 0 {
		return nil, fmt.Errorf("empty credential encoding")
	}
	return document.Content[0], nil
}

// findMappingValue returns the value node for key in a mapping node.
func findMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
