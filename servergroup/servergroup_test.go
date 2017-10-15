package servergroup

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestYaml(t *testing.T) {
	input := `
- http://www.coke.com
- http://localhost
- http://localhost:9090
	`
	input = strings.TrimSpace(input)
	c := &ServerGroup{}

	if err := yaml.Unmarshal([]byte(input), c); err != nil {
		t.Fatalf("Error unmarshal: %v", err)
	}

	if len(c.ResolvedURLs) < len(c.OriginalURLs) {
		t.Fatalf("Unable to do resolve?")
	}
}
