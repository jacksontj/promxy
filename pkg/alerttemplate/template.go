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

// TemplateManager manages template loading and caching from directories and inline templates
type TemplateManager struct {
	mu              sync.RWMutex
	templates       map[string]string // template name -> template content (merged from inline and directory)
	inlineTemplates map[string]string // inline templates from configuration
	directory       string            // template directory path
}

// NewTemplateManager creates a new template manager
func NewTemplateManager() *TemplateManager {
	return &TemplateManager{
		templates:       make(map[string]string),
		inlineTemplates: make(map[string]string),
	}
}

// LoadInlineTemplates loads inline templates from configuration
func (tm *TemplateManager) LoadInlineTemplates(inlineTemplates map[string]string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Clear existing inline templates
	tm.inlineTemplates = make(map[string]string)

	// Validate and load inline templates
	for name, content := range inlineTemplates {
		if err := tm.validateTemplateName(name); err != nil {
			logrus.Errorf("Invalid inline template name '%s': %v", name, err)
			continue
		}

		if err := tm.validateInlineTemplateContent(name, content); err != nil {
			logrus.Errorf("Invalid inline template content for '%s': %v", name, err)
			continue
		}

		tm.inlineTemplates[name] = content
		logrus.Infof("Loaded inline template '%s'", name)
	}

	// Rebuild merged templates
	tm.rebuildTemplates()

	logrus.Infof("Loaded %d inline templates", len(tm.inlineTemplates))
	return nil
}

// LoadFromDirectory loads templates from the specified directory
func (tm *TemplateManager) LoadFromDirectory(directory string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.directory = directory

	// If no directory specified, just rebuild with inline templates
	if directory == "" {
		tm.rebuildTemplates()
		return nil
	}

	// Check if directory exists
	if _, err := os.Stat(directory); os.IsNotExist(err) {
		logrus.Warnf("Template directory does not exist: %s", directory)
		tm.rebuildTemplates()
		return nil
	}

	// Load directory templates
	directoryTemplates := make(map[string]string)

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
		if err := tm.validateTemplateContent(templateName, string(content)); err != nil {
			logrus.Errorf("Invalid template in file %s: %v", path, err)
			return nil // Continue processing other files
		}

		directoryTemplates[templateName] = string(content)
		logrus.Infof("Loaded template '%s' from %s", templateName, path)

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk template directory %s: %w", directory, err)
	}

	// Rebuild merged templates with directory templates taking precedence
	tm.rebuildTemplatesWithDirectory(directoryTemplates)

	logrus.Infof("Loaded %d templates from directory %s", len(directoryTemplates), directory)
	return nil
}

// rebuildTemplates rebuilds the merged template map from inline templates only
func (tm *TemplateManager) rebuildTemplates() {
	tm.templates = make(map[string]string)
	
	// Start with inline templates
	for name, content := range tm.inlineTemplates {
		tm.templates[name] = content
	}
}

// rebuildTemplatesWithDirectory rebuilds the merged template map with directory templates taking precedence
func (tm *TemplateManager) rebuildTemplatesWithDirectory(directoryTemplates map[string]string) {
	tm.templates = make(map[string]string)
	
	// Start with inline templates
	for name, content := range tm.inlineTemplates {
		tm.templates[name] = content
	}
	
	// Directory templates override inline templates
	for name, content := range directoryTemplates {
		if _, exists := tm.inlineTemplates[name]; exists {
			logrus.Infof("Directory template '%s' overrides inline template", name)
		}
		tm.templates[name] = content
	}
}

// validateTemplateName validates a template name
func (tm *TemplateManager) validateTemplateName(name string) error {
	if name == "" {
		return fmt.Errorf("template name cannot be empty")
	}
	
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("template name cannot contain path separators")
	}
	
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("template name cannot start with a dot")
	}
	
	return nil
}

// validateTemplateContent validates template content by attempting to parse it
func (tm *TemplateManager) validateTemplateContent(name, content string) error {
	_, err := template.New(name).Funcs(templateFuncs).Parse(content)
	if err != nil {
		return fmt.Errorf("template parsing failed: %w", err)
	}
	
	return nil
}

// validateInlineTemplateContent validates inline template content (stricter validation)
func (tm *TemplateManager) validateInlineTemplateContent(name, content string) error {
	if content == "" {
		return fmt.Errorf("template content cannot be empty")
	}
	
	return tm.validateTemplateContent(name, content)
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

// GetInlineTemplates returns a copy of the inline templates map
func (tm *TemplateManager) GetInlineTemplates() map[string]string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	result := make(map[string]string)
	for name, content := range tm.inlineTemplates {
		result[name] = content
	}
	return result
}

// HasInlineTemplate checks if an inline template exists
func (tm *TemplateManager) HasInlineTemplate(name string) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	_, exists := tm.inlineTemplates[name]
	return exists
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

// TemplateRule defines conditions for selecting specific templates
type TemplateRule struct {
	// Label selectors to match alerts
	MatchLabels map[string]string `yaml:"match_labels"`
	
	// Template to use for matching alerts (can be template content or template name)
	Template string `yaml:"template"`
}

// SelectTemplate selects the appropriate template for an alert based on rules
// Returns the template content to use, or empty string if no template should be used
func SelectTemplate(rules []TemplateRule, defaultTemplate string, tm *TemplateManager, alert *rules.Alert) string {
	if alert == nil {
		return defaultTemplate
	}
	
	alertLabels := alert.Labels.Map()
	
	// Evaluate rules in order (top-to-bottom matching)
	for _, rule := range rules {
		if matchesRule(rule, alertLabels) {
			// Check if the template is a named template or inline content
			if tm != nil {
				if namedTemplate, exists := tm.GetTemplate(rule.Template); exists {
					return namedTemplate
				}
			}
			
			// Treat as inline template content
			return rule.Template
		}
	}
	
	// No rules matched, use default template
	if defaultTemplate != "" {
		// Check if default template is a named template
		if tm != nil {
			if namedTemplate, exists := tm.GetTemplate(defaultTemplate); exists {
				return namedTemplate
			}
		}
		
		// Treat as inline template content
		return defaultTemplate
	}
	
	return ""
}

// matchesRule checks if an alert's labels match a template rule's match criteria
func matchesRule(rule TemplateRule, alertLabels map[string]string) bool {
	if len(rule.MatchLabels) == 0 {
		return false
	}
	
	// All match labels must be present and have matching values
	for key, expectedValue := range rule.MatchLabels {
		actualValue, exists := alertLabels[key]
		if !exists || actualValue != expectedValue {
			return false
		}
	}
	
	return true
}



