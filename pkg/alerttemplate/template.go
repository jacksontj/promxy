package alerttemplate

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/prometheus/prometheus/rules"
	"github.com/sirupsen/logrus"
)

// TemplateData represents the data available to GeneratorURL templates
type TemplateData struct {
	ExternalURL  string            `json:"external_url"`  // Base URL of the Prometheus instance (e.g., "http://prometheus.example.com:9090")
	Expr         string            `json:"expr"`          // PromQL expression that triggered the alert
	Labels       map[string]string `json:"labels"`        // All alert labels
	Annotations  map[string]string `json:"annotations"`   // All alert annotations
	AlertName    string            `json:"alert_name"`    // Value of the "alertname" label
}

// templateFuncs provides template functions for URL encoding
var templateFuncs = template.FuncMap{
	"urlquery": url.QueryEscape,
	"urlpath":  url.PathEscape,
}

// TemplateManager manages template loading and caching from directories
type TemplateManager struct {
	mu        sync.RWMutex
	templates map[string]string // template name -> template content
	directory string            // template directory path
}

// NewTemplateManager creates a new template manager
func NewTemplateManager() *TemplateManager {
	return &TemplateManager{
		templates: make(map[string]string),
	}
}

// LoadFromDirectory loads templates from the specified directory
func (tm *TemplateManager) LoadFromDirectory(directory string) error {
	if directory == "" {
		return nil
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.directory = directory
	tm.templates = make(map[string]string)

	// Check if directory exists
	if _, err := os.Stat(directory); os.IsNotExist(err) {
		logrus.Warnf("Template directory does not exist: %s", directory)
		return nil
	}

	// Walk through the directory and load template files
	err := filepath.WalkDir(directory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			logrus.Errorf("Error accessing path %s: %v", path, err)
			return nil // Continue processing other files
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Only process .tmpl files
		if !strings.HasSuffix(path, ".tmpl") {
			return nil
		}

		// Read template file
		content, err := os.ReadFile(path)
		if err != nil {
			logrus.Errorf("Failed to read template file %s: %v", path, err)
			return nil // Continue processing other files
		}

		// Generate template name from file path (relative to directory, without .tmpl extension)
		relPath, err := filepath.Rel(directory, path)
		if err != nil {
			logrus.Errorf("Failed to get relative path for %s: %v", path, err)
			return nil
		}

		templateName := strings.TrimSuffix(relPath, ".tmpl")
		// Replace path separators with dots for template names
		templateName = strings.ReplaceAll(templateName, string(filepath.Separator), ".")

		// Validate template content
		if _, err := template.New(templateName).Funcs(templateFuncs).Parse(string(content)); err != nil {
			logrus.Errorf("Invalid template in file %s: %v", path, err)
			return nil // Continue processing other files
		}

		tm.templates[templateName] = string(content)
		logrus.Infof("Loaded template '%s' from %s", templateName, path)

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk template directory %s: %w", directory, err)
	}

	logrus.Infof("Loaded %d templates from directory %s", len(tm.templates), directory)
	return nil
}

// GetTemplate returns the template content by name
func (tm *TemplateManager) GetTemplate(name string) (string, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	template, exists := tm.templates[name]
	return template, exists
}

// ListTemplates returns all available template names
func (tm *TemplateManager) ListTemplates() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	names := make([]string, 0, len(tm.templates))
	for name := range tm.templates {
		names = append(names, name)
	}
	return names
}

// GetDirectory returns the current template directory
func (tm *TemplateManager) GetDirectory() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.directory
}

// ExecuteGeneratorURLTemplate executes a Go template for generating alert URLs
func ExecuteGeneratorURLTemplate(templateStr string, alert *rules.Alert, expr, externalURL string) (string, error) {
	if templateStr == "" {
		// Return empty string to indicate no template was provided
		// Caller should handle fallback logic
		return "", nil
	}
	
	if alert == nil {
		return "", fmt.Errorf("alert cannot be nil")
	}
	
	tmpl, err := template.New("generator").Funcs(templateFuncs).Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}
	
	data := TemplateData{
		ExternalURL: externalURL,
		Expr:        expr,
		Labels:      alert.Labels.Map(),
		Annotations: alert.Annotations.Map(),
		AlertName:   alert.Labels.Get("alertname"),
	}
	
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}
	
	return buf.String(), nil
}

// ExecuteTemplateByName executes a template by name from the template manager
func ExecuteTemplateByName(tm *TemplateManager, templateName string, alert *rules.Alert, expr, externalURL string) (string, error) {
	if tm == nil {
		return "", fmt.Errorf("template manager cannot be nil")
	}
	
	templateStr, exists := tm.GetTemplate(templateName)
	if !exists {
		return "", fmt.Errorf("template '%s' not found", templateName)
	}
	
	return ExecuteGeneratorURLTemplate(templateStr, alert, expr, externalURL)
}

