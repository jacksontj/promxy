# Alert templates

By default alerts carry a Prometheus-style `GeneratorURL`: a link to promxy's
graph page for the alert expression, built from `--web.external-url`.

`promxy.alert_templates` replaces that with your own URLs — a Grafana alert
view, a PagerDuty incident form, a runbook — chosen per alert. Opt-in; omit the
block to keep the default.

## Schema

```yaml
promxy:
  alert_templates:
    # Used when no rule matches. Either an inline template body or the name of
    # one of the `named` templates below.
    default: '{{.ExternalURL}}/graph?g0.expr={{.Expr | urlquery}}&g0.tab=1'

    # Reusable templates, addressable from `default` and from a rule's
    # `template`.
    named:
      grafana: 'https://grafana.example.com/alerting/groups?queryString=alertname%3D%22{{.AlertName | urlquery}}%22'
      pagerduty: 'https://example.pagerduty.com/incidents/new?title={{.AlertName | urlquery}}'

    # Evaluated top-to-bottom; the first rule whose match_labels all match wins.
    rules:
      - match_labels:
          severity: critical
        template: pagerduty
      - match_labels:
          team: frontend
        template: grafana
      - match_labels:
          alertname: DatabaseDown
        template: 'https://db.example.com/status?db={{.Labels.database | urlquery}}'
```

## Selection order

1. Each rule in `rules`, in order. A rule matches when **every** entry in its
   `match_labels` is present on the alert with that exact value. An empty
   `match_labels` never matches — use `default` for a catch-all.
2. `default`, if set.
3. Promxy's built-in Prometheus-style URL.

A rule's `template` (and `default`) is either the name of a `named` template or
an inline template body.

## Template context

Each template is a Go [`text/template`](https://pkg.go.dev/text/template)
rendered once per alert with these fields:

| Field | Type | Description |
| ----- | ---- | ----------- |
| `.ExternalURL` | string | Promxy's external URL (`--web.external-url`). |
| `.Expr` | string | The alerting rule's expression. |
| `.AlertName` | string | The alert name. |
| `.Labels` | map | The alert's label set. |
| `.Annotations` | map | The alert's annotations. |

Two extra functions are available beyond the `text/template` builtins:

- `urlquery` — escape a value for use in a URL query string
- `urlpath` — escape a value for use in a URL path segment

Always pipe interpolated values through one of these — alert labels routinely
contain characters that would otherwise produce a malformed URL.

```
'https://runbooks.example.com/{{.AlertName | urlpath}}?instance={{.Labels.instance | urlquery}}'
```
