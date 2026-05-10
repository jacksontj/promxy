package promclient

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"github.com/jacksontj/promxy/pkg/promhttputil"
)

var (
	allRegex = relabel.MustNewRegexp("(.*)")
)

// MetricRelabelConfig defines relabeling to be done *in-client*. This is a
// significantly constrained version of relabeling (as compared to prometheus ingestion)
// as these all need to be reversible -- as we need to adjust queries on-the-fly
type MetricRelabelConfig struct {
	SourceLabel model.LabelName `yaml:"source_label,flow,omitempty"`
	TargetLabel string          `yaml:"target_label,omitempty"`
	// Actions must either (a) not modify the LabelName or (b) be reversible as we need
	// to modify query/matchers appropriately
	// As such we only support:
	// - replace
	// - labeldrop
	// - lowercase
	// - uppercase
	Action relabel.Action `yaml:"action,omitempty"`
}

// ToRelabelConfig simply converts our simplified relabel configuration into the
// equivalent prometheus relabel config. This is a good method to see exactly how
// these are mapped over.
func (c *MetricRelabelConfig) ToRelabelConfig() (*relabel.Config, error) {
	var cfg *relabel.Config

	switch c.Action {
	case relabel.Replace:
		cfg = &relabel.Config{
			Action:       c.Action,
			Regex:        allRegex,
			SourceLabels: model.LabelNames{c.SourceLabel},
			TargetLabel:  c.TargetLabel,
			Replacement:  "$1",
		}
	case relabel.LabelDrop:
		r, err := relabel.NewRegexp(string(c.SourceLabel))
		if err != nil {
			return nil, err
		}
		cfg = &relabel.Config{
			Action: c.Action,
			Regex:  r,
		}
	case relabel.Lowercase:
		cfg = &relabel.Config{
			Action:       c.Action,
			SourceLabels: model.LabelNames{c.SourceLabel},
			TargetLabel:  c.TargetLabel,
		}
	case relabel.Uppercase:
		cfg = &relabel.Config{
			Action:       c.Action,
			SourceLabels: model.LabelNames{c.SourceLabel},
			TargetLabel:  c.TargetLabel,
		}
	default:
		return nil, fmt.Errorf("unsupported action %s", c.Action)
	}

	// upstream uses the yaml unmarshal method to do their validation (instead of a separate Validate() method)
	// so to get validation complete we do the yaml marshal/unmarshal here
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	tmp := &relabel.Config{}
	if err := yaml.Unmarshal(b, tmp); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *MetricRelabelConfig) Validate() error {
	if c.SourceLabel != "" {
		if !model.IsValidMetricName(model.LabelValue(c.SourceLabel)) {
			return fmt.Errorf("source_label %s must be a valid label name", c.SourceLabel)
		}
	}

	if c.TargetLabel != "" {
		if !model.IsValidMetricName(model.LabelValue(c.TargetLabel)) {
			return fmt.Errorf("target_label %s must be a valid label name", c.TargetLabel)
		}
	}

	switch c.Action {
	case relabel.Replace:
		if c.SourceLabel == "" || c.TargetLabel == "" {
			return fmt.Errorf("relabel configuration for %s action requires SourceLabel and TargetLabel", c.Action)
		}
	case relabel.LabelDrop:
		if c.SourceLabel == "" {
			return fmt.Errorf("relabel configuration for %s action requires SourceLabel", c.Action)
		}
		if c.TargetLabel != "" {
			return fmt.Errorf("'TargetLabel' can not be set for %s action", c.Action)
		}
	case relabel.Lowercase, relabel.Uppercase:
		if c.SourceLabel == "" || c.TargetLabel == "" {
			return fmt.Errorf("relabel configuration for %s action requires SourceLabel and TargetLabel", c.Action)
		}

	default:
		return fmt.Errorf("action %s not supported", c.Action)
	}

	return nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *MetricRelabelConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain MetricRelabelConfig
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if err := c.Validate(); err != nil {
		return err
	}

	return nil
}

// NewMetricsRelabelClient returns a new MetricsRelabelClient which will intercept and rewrite queries
func NewMetricsRelabelClient(a API, cfgs []*MetricRelabelConfig) (*MetricsRelabelClient, error) {
	// Check some cases in relabel config
	for _, cfg := range cfgs {
		// 1) target_label can't have any regex in it
		if strings.Contains(cfg.TargetLabel, "${") {
			return nil, fmt.Errorf("MetricsRelabelClient does not support regex in TargetLabel")
		}
	}

	relabelConfigs := make([]*relabel.Config, len(cfgs))
	for i, cfg := range cfgs {
		relabelCfg, err := cfg.ToRelabelConfig()
		if err != nil {
			return nil, err
		}
		relabelConfigs[i] = relabelCfg
	}

	return &MetricsRelabelClient{a, cfgs, relabelConfigs}, nil
}

// MetricsRelabelClient
type MetricsRelabelClient struct {
	API
	MetricsRelabelConfigs []*MetricRelabelConfig
	RelabelConfigs        []*relabel.Config
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (c *MetricsRelabelClient) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	newMatchers := make([]string, len(matchers))
	for i, s := range matchers {
		matchers, err := parser.ParseMetricSelector(s)
		if err != nil {
			return nil, nil, err
		}
		rewriteMatchers, ok := RewriteMatchers(c.MetricsRelabelConfigs, matchers)
		if !ok {
			return nil, nil, nil
		}

		newMatchers[i], err = promhttputil.MatcherToString(rewriteMatchers)
		if err != nil {
			return nil, nil, err
		}
	}

	labelNames, w, err := c.API.LabelNames(ctx, newMatchers, startTime, endTime)
	if err != nil {
		return nil, w, err
	}

	// Now that we have the result; we need to run the relabel on the lbls
	lb := labels.NewScratchBuilder(len(labelNames))
	for _, lName := range labelNames {
		lb.Add(lName, "placeholder")
	}
	lb.Sort()
	lbls, keep := relabel.Process(lb.Labels(), c.RelabelConfigs...)
	if !keep {
		return nil, w, err
	}

	newLabelNames := make([]string, 0, lbls.Len())
	lbls.Range(func(lbl labels.Label) {
		newLabelNames = append(newLabelNames, lbl.Name)
	})

	return newLabelNames, w, err
}

// Query performs a query for the given time.
func (c *MetricsRelabelClient) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	// rewrite the query to the new labels
	logrus.Debugf("Query before label replacement: %s", query)
	e, err := parser.ParseExpr(query)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	// Walk the expression, to filter out any LabelMatchers that match etc.
	replaceVisitor := NewMetricsRelabelVisitor(c.MetricsRelabelConfigs, c.RelabelConfigs)
	if _, err := parser.Walk(ctx, replaceVisitor, &parser.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return storage.ErrSeriesSet(err)
	}
	if replaceVisitor.badLabel {
		return storage.EmptySeriesSet()
	}

	newQuery := e.String()
	logrus.Debugf("Query after label replacement: %s", newQuery)

	return MapLabelsSeriesSet(c.API.Query(ctx, newQuery, ts), c.relabelLabels)
}

// relabelLabels applies the relabel configs to a series' labels (the result-side
// of the rewrite). A dropped series (keep=false) is rendered with empty labels,
// matching the previous model.Value behavior.
func (c *MetricsRelabelClient) relabelLabels(l labels.Labels) labels.Labels {
	lbls, keep := relabel.Process(l, c.RelabelConfigs...)
	if !keep {
		return labels.EmptyLabels()
	}
	return lbls
}

// QueryRange performs a query for the given range.
func (c *MetricsRelabelClient) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	// rewrite the query to the new labels
	logrus.Debugf("Query before label replacement: %s", query)
	e, err := parser.ParseExpr(query)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	// Walk the expression, to filter out any LabelMatchers that match etc.
	replaceVisitor := NewMetricsRelabelVisitor(c.MetricsRelabelConfigs, c.RelabelConfigs)
	if _, err := parser.Walk(ctx, replaceVisitor, &parser.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return storage.ErrSeriesSet(err)
	}
	if replaceVisitor.badLabel {
		return storage.EmptySeriesSet()
	}

	newQuery := e.String()
	logrus.Debugf("Query after label replacement: %s", newQuery)

	return MapLabelsSeriesSet(c.API.QueryRange(ctx, newQuery, r), c.relabelLabels)
}

// Series finds series by label matchers.
func (c *MetricsRelabelClient) Series(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	newMatchers := make([]string, len(matchers))
	for i, s := range matchers {
		matchers, err := parser.ParseMetricSelector(s)
		if err != nil {
			return nil, nil, err
		}
		rewriteMatchers, ok := RewriteMatchers(c.MetricsRelabelConfigs, matchers)
		if !ok {
			return nil, nil, nil
		}

		newMatchers[i], err = promhttputil.MatcherToString(rewriteMatchers)
		if err != nil {
			return nil, nil, err
		}
	}

	labelsets, w, err := c.API.Series(ctx, newMatchers, startTime, endTime)
	for i, labelset := range labelsets {
		lb := labels.NewScratchBuilder(len(labelset))
		for k, v := range labelset {
			lb.Add(string(k), string(v))
		}
		lb.Sort()
		lbls, keep := relabel.Process(lb.Labels(), c.RelabelConfigs...)
		if !keep {
			labelsets[i] = nil
			continue
		}
		newLabelset := make(model.LabelSet, lbls.Len())
		lbls.Range(func(lbl labels.Label) {
			newLabelset[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
		})
		labelsets[i] = newLabelset
	}
	return labelsets, w, err
}

// GetValue loads the raw data for a given set of matchers in the time range
func (c *MetricsRelabelClient) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	newMatchers, ok := RewriteMatchers(c.MetricsRelabelConfigs, matchers)
	if !ok {
		return storage.EmptySeriesSet()
	}
	return MapLabelsSeriesSet(c.API.GetValue(ctx, start, end, newMatchers), c.relabelLabels)
}

// QueryExemplars performs a query for exemplars by the given query and time range.
// We pass the query through unmodified — rewriting selectors inside an arbitrary
// PromQL expression is non-trivial — and apply the configured relabel rules to
// each result's SeriesLabels so caller-side labels match the rewritten metric
// names.
func (c *MetricsRelabelClient) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	v, err := c.API.QueryExemplars(ctx, query, startTime, endTime)
	if err != nil {
		return nil, err
	}
	if len(c.RelabelConfigs) == 0 {
		return v, nil
	}
	out := v[:0]
	for _, qr := range v {
		labelStrings := make([]string, 0, len(qr.SeriesLabels)*2)
		for k, lv := range qr.SeriesLabels {
			labelStrings = append(labelStrings, string(k), string(lv))
		}
		lbls, keep := relabel.Process(labels.FromStrings(labelStrings...), c.RelabelConfigs...)
		if !keep {
			continue
		}
		ns := model.LabelSet{}
		lbls.Range(func(lbl labels.Label) {
			ns[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
		})
		qr.SeriesLabels = ns
		out = append(out, qr)
	}
	return out, nil
}

func NewMetricsRelabelVisitor(m []*MetricRelabelConfig, r []*relabel.Config) *MetricsRelabelVisitor {
	return &MetricsRelabelVisitor{
		MetricsRelabelConfigs: m,
		RelabelConfigs:        r,
	}
}

// MetricsRelabelVisitor implements the parser.Visitor interface to replace the labels
type MetricsRelabelVisitor struct {
	MetricsRelabelConfigs []*MetricRelabelConfig
	RelabelConfigs        []*relabel.Config

	l        sync.Mutex
	badLabel bool
}

func (v *MetricsRelabelVisitor) Visit(node parser.Node, path []parser.Node) (w parser.Visitor, err error) {
	switch nodeTyped := node.(type) {
	case *parser.VectorSelector:
		var ok bool
		nodeTyped.LabelMatchers, ok = RewriteMatchers(v.MetricsRelabelConfigs, nodeTyped.LabelMatchers)
		if !ok {
			v.l.Lock()
			v.badLabel = true
			v.l.Unlock()
			return nil, nil // Stop iteration
		}
	case *parser.MatrixSelector:
		var ok bool
		nodeTyped.VectorSelector.(*parser.VectorSelector).LabelMatchers, ok = RewriteMatchers(v.MetricsRelabelConfigs, nodeTyped.VectorSelector.(*parser.VectorSelector).LabelMatchers)
		if !ok {
			v.l.Lock()
			v.badLabel = true
			v.l.Unlock()
			return nil, nil // Stop iteration
		}
	case *parser.AggregateExpr:
		nodeTyped.Grouping = RewriteLabels(v.MetricsRelabelConfigs, nodeTyped.Grouping)
	case *parser.BinaryExpr:
		// If one is a literal; then it is safe to traverse
		if ExprIsLiteral(nodeTyped.LHS) || ExprIsLiteral(nodeTyped.RHS) {
			return v, nil
		}
		return nil, fmt.Errorf("metricsrelabelvisitor does not support BinaryExprs")
	}

	return v, nil
}

// RewriteLabels simply rewrites the label names passed in if the actions require it
func RewriteLabels(cfgs []*MetricRelabelConfig, labels []string) []string {
	replacedLabels := make([]string, 0, len(labels))
	for _, label := range labels {
		for x := len(cfgs) - 1; x >= 0; x-- {
			cfg := cfgs[x]
			switch cfg.Action {
			// For `Replace` we simply need to rewrite the matchers from the "new" LabelName to the "old" LabelName
			case relabel.Replace:
				if label == cfg.TargetLabel {
					label = string(cfg.SourceLabel)
				}
				// For `LabelDrop` we don't replace the name (as it was dropped, not changed)
			case relabel.LabelDrop:
				if label == string(cfg.SourceLabel) {
					label = ""
				}

				// These only need to do something if its a rewrite; otherwise we just continue on
			case relabel.Lowercase, relabel.Uppercase:
				// If this is a *new* labelname then we need to do something about it
				if string(cfg.SourceLabel) != cfg.TargetLabel {
					if label == string(cfg.TargetLabel) {
						label = string(cfg.SourceLabel)
					}
				}
			}
		}
		if label != "" {
			replacedLabels = append(replacedLabels, label)
		}
	}

	return replacedLabels
}

const caseInsensitiveRegexPrefix = "(?i)"

// RewriteMatchers go through each matcher and replace the label if matches
func RewriteMatchers(cfgs []*MetricRelabelConfig, matchers []*labels.Matcher) ([]*labels.Matcher, bool) {
	replacedMatchers := make([]*labels.Matcher, len(matchers))

	// Look over the matchers passed in, if any exist in our labels, we'll do the matcher, and then strip
	for i, originalMatcher := range matchers {
		replacedMatchers[i] = originalMatcher
		// We need to reverse iterate over the configs as we are rewriting to ensure the rewrites
		// are layerd properly
		for x := len(cfgs) - 1; x >= 0; x-- {
			cfg := cfgs[x]
			switch cfg.Action {
			// For `Replace` we simply need to rewrite the matchers from the "new" LabelName to the "old" LabelName
			case relabel.Replace:
				if replacedMatchers[i].Name == cfg.TargetLabel {
					replacedMatchers[i] = &labels.Matcher{
						Type:  replacedMatchers[i].Type,
						Name:  string(cfg.SourceLabel),
						Value: replacedMatchers[i].Value,
					}
				}
				// For `LabelDrop`, we already know nothing will match this -- so we want to return nothing and not query
				// the downstream (assuming the matcher expects *anything*)
			case relabel.LabelDrop:
				if replacedMatchers[i].Name == string(cfg.SourceLabel) {
					// Exceptional cases here are if its expecting ""
					if replacedMatchers[i].Type == labels.MatchEqual && replacedMatchers[i].Value == "" {
						continue
					}
					// or !~ .+
					if replacedMatchers[i].Type == labels.MatchNotRegexp && replacedMatchers[i].Value == ".+" {
						continue
					}

					return nil, false
				}

				// For both case changes we need to simply change all our matchers to be case insensitive versions
			case relabel.Lowercase, relabel.Uppercase:
				if replacedMatchers[i].Name == cfg.TargetLabel {
					replacedMatchers[i] = &labels.Matcher{
						Type:  replacedMatchers[i].Type,
						Name:  string(cfg.SourceLabel),
						Value: caseInsensitiveRegexPrefix + replacedMatchers[i].Value,
					}
					switch replacedMatchers[i].Type {
					case labels.MatchEqual:
						replacedMatchers[i].Type = labels.MatchRegexp
					case labels.MatchNotEqual:
						replacedMatchers[i].Type = labels.MatchNotRegexp
					}
				}
			}
		}
	}

	return replacedMatchers, true
}
