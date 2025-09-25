package alerttemplate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/rules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplateManager_LoadFromDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "template_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test files including nested directories and non-template files
	testFiles := map[string]string{
		"grafana.tmpl":         "https://grafana.example.com/alert/{{.AlertName}}",
		"subdir/custom.tmpl":   "https://custom.example.com/{{.Labels.severity}}",
		"invalid.tmpl":         "https://example.com/{{.InvalidSyntax", // Invalid template
		"readme.txt":           "This is not a template",               // Non-template file
	}

	for filePath, content := range testFiles {
		fullPath := filepath.Join(tempDir, filePath)
		dir := filepath.Dir(fullPath)
		if dir != tempDir {
			err := os.MkdirAll(dir, 0755)
			require.NoError(t, err)
		}
		err := os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err)
	}

	tm := NewTemplateManager()
	err = tm.LoadFromDirectory(tempDir)
	require.NoError(t, err)

	// Should load 2 valid templates (grafana and subdir.custom)
	// Invalid template and non-template files should be skipped
	names := tm.ListTemplates()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "grafana")
	assert.Contains(t, names, "subdir.custom")

	// Verify template content
	content, exists := tm.GetTemplate("grafana")
	assert.True(t, exists)
	assert.Equal(t, "https://grafana.example.com/alert/{{.AlertName}}", content)
}

func TestTemplateManager_EdgeCases(t *testing.T) {
	tm := NewTemplateManager()
	
	// Empty path should not error
	err := tm.LoadFromDirectory("")
	assert.NoError(t, err)
	assert.Len(t, tm.ListTemplates(), 0)
	
	// Non-existent directory should not error
	err = tm.LoadFromDirectory("/non/existent/directory")
	assert.NoError(t, err)
	assert.Len(t, tm.ListTemplates(), 0)
}

func TestExecuteTemplateByName(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "template_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	templateContent := "https://grafana.example.com/alert/{{.AlertName}}?severity={{.Labels.severity|urlquery}}"
	err = os.WriteFile(filepath.Join(tempDir, "grafana.tmpl"), []byte(templateContent), 0644)
	require.NoError(t, err)

	tm := NewTemplateManager()
	err = tm.LoadFromDirectory(tempDir)
	require.NoError(t, err)

	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname": "HighCPU",
			"severity":  "critical",
		}),
	}

	// Test successful execution
	result, err := ExecuteTemplateByName(tm, "grafana", alert, "up == 0", "http://prometheus:9090")
	assert.NoError(t, err)
	assert.Equal(t, "https://grafana.example.com/alert/HighCPU?severity=critical", result)

	// Test error cases
	_, err = ExecuteTemplateByName(tm, "nonexistent", alert, "up == 0", "http://prometheus:9090")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "template 'nonexistent' not found")

	_, err = ExecuteTemplateByName(nil, "grafana", alert, "up == 0", "http://prometheus:9090")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "template manager cannot be nil")
}

func TestTemplateManager_ConcurrentAccess(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "template_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	err = os.WriteFile(filepath.Join(tempDir, "test.tmpl"), []byte("test content"), 0644)
	require.NoError(t, err)

	tm := NewTemplateManager()
	err = tm.LoadFromDirectory(tempDir)
	require.NoError(t, err)

	// Test concurrent reads
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func() {
			defer func() { done <- true }()
			_, exists := tm.GetTemplate("test")
			assert.True(t, exists)
			assert.Len(t, tm.ListTemplates(), 1)
		}()
	}
	
	for i := 0; i < 5; i++ {
		<-done
	}
}
