package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type prometheusTypeSet map[string]struct{}

func (s prometheusTypeSet) write(b *strings.Builder, name string, kind string) {
	if _, ok := s[name]; ok {
		return
	}
	s[name] = struct{}{}
	fmt.Fprintf(b, "# TYPE %s %s\n", name, kind)
}

func writePrometheusSeries(b *strings.Builder, series Series, types prometheusTypeSet) {
	name := prometheusName(series.Name)
	labels := prometheusLabels(series.Labels)
	switch series.Kind {
	case KindCounter:
		if series.Value == nil {
			return
		}
		types.write(b, "hoopoe_"+name, "counter")
		fmt.Fprintf(b, "hoopoe_%s%s %s\n", name, labels, formatFloat(*series.Value))
	case KindGauge:
		if series.Value == nil {
			return
		}
		types.write(b, "hoopoe_"+name, "gauge")
		fmt.Fprintf(b, "hoopoe_%s%s %s\n", name, labels, formatFloat(*series.Value))
	case KindHistogram:
		writePrometheusHistogram(b, name, labels, series, types)
	}
}

func writePrometheusHistogram(b *strings.Builder, name string, labels string, series Series, types prometheusTypeSet) {
	types.write(b, "hoopoe_"+name+"_count", "counter")
	fmt.Fprintf(b, "hoopoe_%s_count%s %d\n", name, labels, series.Count)
	types.write(b, "hoopoe_"+name+"_sum", "counter")
	fmt.Fprintf(b, "hoopoe_%s_sum%s %s\n", name, labels, formatFloat(series.Sum))
	if series.P95 != nil {
		types.write(b, "hoopoe_"+name+"_p95", "gauge")
		fmt.Fprintf(b, "hoopoe_%s_p95%s %s\n", name, labels, formatFloat(*series.P95))
	}
	if series.Max != nil {
		types.write(b, "hoopoe_"+name+"_max", "gauge")
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
