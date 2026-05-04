package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func writePrometheusSeries(b *strings.Builder, series Series) {
	name := prometheusName(series.Name)
	labels := prometheusLabels(series.Labels)
	switch series.Kind {
	case KindCounter:
		if series.Value == nil {
			return
		}
		fmt.Fprintf(b, "# TYPE hoopoe_%s counter\n", name)
		fmt.Fprintf(b, "hoopoe_%s%s %s\n", name, labels, formatFloat(*series.Value))
	case KindGauge:
		if series.Value == nil {
			return
		}
		fmt.Fprintf(b, "# TYPE hoopoe_%s gauge\n", name)
		fmt.Fprintf(b, "hoopoe_%s%s %s\n", name, labels, formatFloat(*series.Value))
	case KindHistogram:
		writePrometheusHistogram(b, name, labels, series)
	}
}

func writePrometheusHistogram(b *strings.Builder, name string, labels string, series Series) {
	fmt.Fprintf(b, "# TYPE hoopoe_%s_count counter\n", name)
	fmt.Fprintf(b, "hoopoe_%s_count%s %d\n", name, labels, series.Count)
	fmt.Fprintf(b, "# TYPE hoopoe_%s_sum counter\n", name)
	fmt.Fprintf(b, "hoopoe_%s_sum%s %s\n", name, labels, formatFloat(series.Sum))
	if series.P95 != nil {
		fmt.Fprintf(b, "# TYPE hoopoe_%s_p95 gauge\n", name)
		fmt.Fprintf(b, "hoopoe_%s_p95%s %s\n", name, labels, formatFloat(*series.P95))
	}
	if series.Max != nil {
		fmt.Fprintf(b, "# TYPE hoopoe_%s_max gauge\n", name)
		fmt.Fprintf(b, "hoopoe_%s_max%s %s\n", name, labels, formatFloat(*series.Max))
	}
}

func prometheusName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == ':':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func prometheusLabels(labels Labels) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, prometheusName(key)+`="`+escapeLabelValue(labels[key])+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func escapeLabelValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
