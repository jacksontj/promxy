// Package expfmt provides a fast, allocation-light encoder for the Prometheus
// text exposition format.
//
// It exists for promxy's /federate endpoint (issue #784). The upstream
// federation handler builds a full dto.MetricFamily tree per request and hands
// it to github.com/prometheus/common/expfmt, whose text encoder (since
// common v0.65 / the Prometheus 3.x UTF-8 name support) re-validates every
// metric and label name on encode. Together that dominates federation CPU and
// allocations. promxy already has the data as labels.Labels (it just decoded a
// downstream response), so it can stream the exposition format directly,
// skipping the dto tree.
//
// The output is byte-for-byte identical to common/expfmt for the subset
// federation emits: UNTYPED float samples. In particular, name handling
// (legacy-valid names written bare, otherwise quoted with the metric name moved
// inside the braces), label-value escaping, and float/timestamp formatting all
// match common/expfmt exactly, including UTF-8 names. This is verified by
// byte-equivalence tests against common/expfmt.
package expfmt

import (
	"bufio"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// numBufPool holds scratch buffers for formatting floats/ints, mirroring
// common/expfmt: a per-call stack buffer escapes to the heap once its slice
// flows into w.Write, so a pool keeps number formatting allocation-free.
var numBufPool = sync.Pool{New: func() any { b := make([]byte, 0, 24); return &b }}

// Replacers mirror common/expfmt: '\' -> '\\', '\n' -> '\n', and (for quoted
// strings, i.e. label values and quoted names) '"' -> '\"'.
var (
	escaper       = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	quotedEscaper = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
)

// writeName writes a metric or label name: bare if it is a legacy-valid name,
// otherwise quoted and escaped. Mirrors common/expfmt's writeName.
func writeName(w *bufio.Writer, name string) {
	if model.IsValidLegacyMetricName(name) {
		_, _ = w.WriteString(name)
		return
	}
	_ = w.WriteByte('"')
	writeEscaped(w, name, true)
	_ = w.WriteByte('"')
}

// writeEscaped writes s with the text-format escaping. quote selects whether
// double quotes are escaped (true for label values and quoted names). A
// fast-path avoids the Replacer when there is nothing to escape.
func writeEscaped(w *bufio.Writer, s string, quote bool) {
	if quote {
		if !strings.ContainsAny(s, "\\\n\"") {
			_, _ = w.WriteString(s)
			return
		}
		_, _ = quotedEscaper.WriteString(w, s)
		return
	}
	if !strings.ContainsAny(s, "\\\n") {
		_, _ = w.WriteString(s)
		return
	}
	_, _ = escaper.WriteString(w, s)
}

// writeFloat mirrors common/expfmt's writeFloat: hardcoded common cases, then
// strconv.AppendFloat with a stack buffer to avoid allocation.
func writeFloat(w *bufio.Writer, f float64) {
	switch {
	case f == 1:
		_ = w.WriteByte('1')
	case f == 0:
		_ = w.WriteByte('0')
	case f == -1:
		_, _ = w.WriteString("-1")
	case math.IsNaN(f):
		_, _ = w.WriteString("NaN")
	case math.IsInf(f, +1):
		_, _ = w.WriteString("+Inf")
	case math.IsInf(f, -1):
		_, _ = w.WriteString("-Inf")
	default:
		bp := numBufPool.Get().(*[]byte)
		*bp = strconv.AppendFloat((*bp)[:0], f, 'g', -1, 64)
		_, _ = w.Write(*bp)
		numBufPool.Put(bp)
	}
}

// writeInt writes an int64 (used for timestamps) without allocating.
func writeInt(w *bufio.Writer, i int64) {
	bp := numBufPool.Get().(*[]byte)
	*bp = strconv.AppendInt((*bp)[:0], i, 10)
	_, _ = w.Write(*bp)
	numBufPool.Put(bp)
}

// metricLine writes the "name{labels}" portion of a sample line, replicating
// common/expfmt's writeNameAndLabelPairs: if the metric name is not
// legacy-valid it is moved inside the braces as the first (quoted) token.
type metricLine struct {
	w         *bufio.Writer
	separator byte
	braces    bool // an opening brace has been written
}

func beginMetric(w *bufio.Writer, name string) metricLine {
	m := metricLine{w: w, separator: '{'}
	if name != "" {
		if !model.IsValidLegacyMetricName(name) {
			m.braces = true
			_ = w.WriteByte('{')
			m.separator = ','
		}
		writeName(w, name)
	}
	return m
}

// label writes a single label pair, opening the brace on the first pair.
func (m *metricLine) label(name, value string) {
	_ = m.w.WriteByte(m.separator)
	if m.separator == '{' {
		m.braces = true
	}
	writeName(m.w, name)
	_, _ = m.w.WriteString(`="`)
	writeEscaped(m.w, value, true)
	_ = m.w.WriteByte('"')
	m.separator = ','
}

func (m *metricLine) end() {
	if m.braces {
		_ = m.w.WriteByte('}')
	}
}

// MetricFamilyToText encodes an UNTYPED metric family to the text format,
// byte-for-byte compatible with common/expfmt for that type. It exists mainly
// as a testing/benchmarking surface that shares the exact primitives the
// federation Encoder uses; the federation handler uses Encoder directly.
func MetricFamilyToText(out io.Writer, mf *dto.MetricFamily) error {
	w, ok := out.(*bufio.Writer)
	ownBuf := false
	if !ok {
		w = bufio.NewWriter(out)
		ownBuf = true
	}

	name := mf.GetName()
	if mf.Help != nil {
		_, _ = w.WriteString("# HELP ")
		writeName(w, name)
		_ = w.WriteByte(' ')
		writeEscaped(w, mf.GetHelp(), false)
		_ = w.WriteByte('\n')
	}
	_, _ = w.WriteString("# TYPE ")
	writeName(w, name)
	_, _ = w.WriteString(" untyped\n")

	for _, m := range mf.GetMetric() {
		ml := beginMetric(w, name)
		for _, lp := range m.GetLabel() {
			ml.label(lp.GetName(), lp.GetValue())
		}
		ml.end()
		_ = w.WriteByte(' ')
		writeFloat(w, m.GetUntyped().GetValue())
		if m.TimestampMs != nil {
			_ = w.WriteByte(' ')
			writeInt(w, m.GetTimestampMs())
		}
		_ = w.WriteByte('\n')
	}

	if ownBuf {
		return w.Flush()
	}
	return nil
}

// Encoder streams federation output (UNTYPED float samples) directly from
// labels.Labels, without building a dto.MetricFamily. Samples must be supplied
// grouped by metric name (sorted), matching upstream federation which emits one
// "# TYPE <name> untyped" line per metric family.
//
// scheme is the name-escaping scheme negotiated from the scrape request's
// Accept header (model.EscapingScheme), applied to every metric and label name
// exactly as common/expfmt's encoder does via model.EscapeMetricFamily. For the
// default (UnderscoreEscaping) and legacy-valid names this is allocation-free.
type Encoder struct {
	w        *bufio.Writer
	scheme   model.EscapingScheme
	lastName string
	started  bool
}

func NewEncoder(w io.Writer, scheme model.EscapingScheme) *Encoder {
	return &Encoder{w: bufio.NewWriter(w), scheme: scheme}
}

// WriteFloatSample writes one untyped sample.
//
//   - name is the metric name (the value of the __name__ label).
//   - lbls is the full label set of the series; the __name__ label and any
//     empty-valued labels are skipped (matching upstream federation).
//   - external are additional labels (e.g. external_labels) to attach, in the
//     order given, but only when not already present in lbls. Pass them
//     pre-sorted by name to match upstream byte ordering.
//
// A "# TYPE <name> untyped" line is emitted whenever name changes from the
// previous call, so callers must group samples by name.
func (e *Encoder) WriteFloatSample(name string, lbls labels.Labels, external []labels.Label, v float64, t int64) {
	name = model.EscapeName(name, e.scheme)
	if !e.started || name != e.lastName {
		_, _ = e.w.WriteString("# TYPE ")
		writeName(e.w, name)
		_, _ = e.w.WriteString(" untyped\n")
		e.lastName = name
		e.started = true
	}

	// Inlined metric-line writing (rather than the metricLine helper) so the
	// lbls.Range closure does not capture a pointer-to-struct and escape to the
	// heap. We also capture only locals (not the receiver e) so escape analysis
	// keeps the closure on the stack -- this keeps the encoder allocation-free
	// per sample.
	w := e.w
	scheme := e.scheme
	sep := byte('{')
	braces := false
	if !model.IsValidLegacyMetricName(name) {
		braces = true
		_ = w.WriteByte('{')
		sep = ','
	}
	writeName(w, name)
	lbls.Range(func(l labels.Label) {
		if l.Name == labels.MetricName || l.Value == "" {
			return
		}
		_ = w.WriteByte(sep)
		braces = true
		sep = ','
		writeName(w, model.EscapeName(l.Name, scheme))
		_, _ = w.WriteString(`="`)
		writeEscaped(w, l.Value, true)
		_ = w.WriteByte('"')
	})
	for _, el := range external {
		// Upstream only suppresses an external label when the series carries
		// that name with a non-empty value (empty-valued series labels are
		// dropped before the "globalUsed" check), so mirror that here.
		if lbls.Get(el.Name) != "" {
			continue
		}
		_ = w.WriteByte(sep)
		braces = true
		sep = ','
		writeName(w, model.EscapeName(el.Name, e.scheme))
		_, _ = w.WriteString(`="`)
		writeEscaped(w, el.Value, true)
		_ = w.WriteByte('"')
	}
	if braces {
		_ = w.WriteByte('}')
	}

	_ = e.w.WriteByte(' ')
	writeFloat(e.w, v)
	_ = e.w.WriteByte(' ')
	writeInt(e.w, t)
	_ = e.w.WriteByte('\n')
}

// Flush flushes any buffered output to the underlying writer.
func (e *Encoder) Flush() error { return e.w.Flush() }
