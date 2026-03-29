# Configurable Generator URL Templates

## Overview

By default, Promxy generates alert URLs that link back to the Prometheus expression browser. With configurable generator URL templates, you can customize these URLs to point to external systems like Grafana dashboards, custom monitoring tools, or any other relevant destination.

## Configuration

### Basic Configuration

Add alert template configuration to your Promxy configuration file:

```yaml
promxy:
  alert_templates:
    default: "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}"
```

### Advanced Configuration Examples

```yaml
promxy:
  alert_templates:
    # Default template used when no rules match
    default: "{{.ExternalURL}}/graph?g0.expr={{.Expr|urlquery}}&g0.tab=1"
    
    # Directory containing template files (*.tmpl)
    directory: "/etc/promxy/templates"
    
    # Named inline templates
    named:
      grafana: "https://grafana.example.com/d/alerts?alertname={{.AlertName|urlquery}}&severity={{.Labels.severity|urlquery}}"
      pagerduty: "https://pagerduty.example.com/incidents/new?title={{.AlertName|urlquery}}&description={{.Annotations.summary|urlquery}}"
      custom_dashboard: "https://monitoring.example.com/alerts/{{.AlertName}}?instance={{.Labels.instance|urlpath}}"
    
    # Template selection rules (evaluated top-to-bottom)
    rules:
      # Critical alerts go to PagerDuty
      - match_labels:
          severity: "critical"
        template: "pagerduty"
      
      # Frontend team alerts go to Grafana
      - match_labels:
          team: "frontend"
        template: "grafana"
      
      # Infrastructure alerts use custom dashboard
      - match_labels:
          component: "infrastructure"
        template: "custom_dashboard"
      
      # Inline template for specific alert
      - match_labels:
          alertname: "DatabaseDown"
        template: "https://db-dashboard.example.com/status?db={{.Labels.database|urlquery}}"
```

#### Template Directory Structure

When using `directory: "/etc/promxy/templates"`, organize templates as:

```
/etc/promxy/templates/
├── grafana.tmpl
├── pagerduty.tmpl
├── custom.tmpl
└── teams/
    ├── frontend.tmpl
    └── backend.tmpl
```

Template files are referenced by their relative path without the `.tmpl` extension:
- `grafana.tmpl` → referenced as `"grafana"`
- `teams/frontend.tmpl` → referenced as `"teams.frontend"`

#### Environment-Specific Configuration

**Development Environment:**
```yaml
promxy:
  alert_templates:
    default: "http://localhost:3000/alerts?alertname={{.AlertName|urlquery}}"
```

**Production Environment:**
```yaml
promxy:
  alert_templates:
    default: "https://grafana.prod.example.com/alerting/groups"
    named:
      oncall: "https://pagerduty.example.com/incidents/new?title={{.AlertName|urlquery}}"
    rules:
      - match_labels:
          severity: "critical"
        template: "oncall"
```

### CLI Override

You can override the configuration file template using CLI flags:

```bash
# Override default template
promxy --rules.alert.generator-url-template="https://custom.example.com/alert/{{.AlertName}}"

# Override template directory
promxy --rules.alert.template-dir="/custom/templates"

# Both overrides
promxy \
  --rules.alert.generator-url-template="https://emergency.example.com/alert/{{.AlertName}}" \
  --rules.alert.template-dir="/emergency/templates"
```

CLI flags take precedence over configuration file settings.

## Template Syntax

Templates use Go's `text/template` syntax with alert data and URL encoding functions.

### Template Data Structure

```go
type TemplateData struct {
    ExternalURL  string            // Base URL: "http://prometheus.example.com:9090"
    Expr         string            // PromQL expression: "up{job=\"web\"} < 1"
    Labels       map[string]string // Alert labels: {"alertname": "HighCPU", "severity": "critical"}
    Annotations  map[string]string // Alert annotations: {"summary": "High CPU detected"}
    AlertName    string            // Shortcut for .Labels.alertname
}
```

### Template Functions

| Function | Purpose | Example Usage | Result |
|----------|---------|---------------|--------|
| `urlquery` | Encode for query parameters | `{{.AlertName\|urlquery}}` | `High+CPU` |
| `urlpath` | Encode for URL paths | `{{.Labels.instance\|urlpath}}` | `server%3A8080` |


## Behavior

### Template Execution
- Templates are executed for each firing alert (pending alerts are not sent to Alertmanager)
- Template execution happens during the alert sending process
- All alert labels and annotations are available to the template

### Error Handling
- If template parsing fails, an error is logged and the default URL is used
- If template execution fails, an error is logged and the default URL is used
- The system continues to function normally even with template errors

### Fallback Behavior
When no template is configured or template execution fails, Promxy falls back to the default Prometheus-style URL:
```
http://prometheus.example.com:9090/graph?g0.expr=up%7Bjob%3D%22web%22%7D&g0.tab=1
```

### Configuration Reloading
- Template changes are applied when the configuration is reloaded (SIGHUP)
- CLI overrides remain in effect after configuration reloads
- No restart is required for template changes

## Configuration Validation

Promxy validates alert template configuration at startup and during configuration reloads. The following validation rules apply:

### Template Directory Validation

- Directory must exist and be readable
- Directory must contain at least one `.tmpl` file (warning if empty)
- All `.tmpl` files must be readable and contain valid template syntax
- Template files are validated for Go template syntax

### Template Name Validation

- Template names cannot be empty
- Template names cannot contain path separators (`/` or `\`)
- Template names cannot start with a dot (`.`)
- Template names must be unique within inline templates

### Template Content Validation

- Template content cannot be empty
- Templates must parse successfully using Go's `text/template` package

### Template Rule Validation

- Each rule must have at least one `match_labels` entry
- Label keys and values in `match_labels` cannot be empty
- Template references in rules must either:
  - Reference a valid named template, or
  - Contain valid inline template content
- Rules are evaluated in the order they appear in configuration
