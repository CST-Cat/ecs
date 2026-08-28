package probe

import "ecs/internal/model"

func ipQualityTableKey(version, kind string) string {
	return "network.ipquality.ipv" + version + "." + kind
}

func (bundle ipQualityBundle) typeTable() model.Table {
	table := model.Table{
		Key:   ipQualityTableKey(bundle.Version, "types"),
		Title: "probe.network.table.ipquality.types",
		Columns: []model.TableColumn{
			{Key: "source", Label: "probe.network.column.source"},
			{Key: "usage_type", Label: "probe.network.column.usage"},
			{Key: "company_type", Label: "probe.network.column.company"},
			{Key: "country", Label: "probe.network.column.country"},
			{Key: "channel", Label: "probe.network.column.channel"},
		},
		RowIdentity: "source",
	}
	for _, id := range typeSourceOrder {
		finding := bundle.Findings[id]
		table.Rows = append(table.Rows, []model.Value{
			model.KeyValue(networkSourceNameKey(id)),
			findingNormalizedValue(finding, finding.Usage),
			findingNormalizedValue(finding, finding.Company),
			findingRawValue(finding, finding.Country),
			findingAccessValue(finding),
		})
	}
	return table
}

func (bundle ipQualityBundle) scoreTable() model.Table {
	table := model.Table{
		Key:   ipQualityTableKey(bundle.Version, "scores"),
		Title: "probe.network.table.ipquality.scores",
		Columns: []model.TableColumn{
			{Key: "source", Label: "probe.network.column.source"},
			{Key: "raw_or_equivalent_value", Label: "probe.network.column.raw_value"},
			{Key: "risk_level", Label: "probe.network.column.risk"},
			{Key: "visualization", Label: "probe.network.column.visualization"},
			{Key: "metric_definition", Label: "probe.network.column.definition"},
			{Key: "bucket_rule", Label: "probe.network.column.bucket"},
			{Key: "channel", Label: "probe.network.column.channel"},
		},
		RowIdentity: "source",
	}
	for _, id := range scoreSourceOrder {
		finding := bundle.Findings[id]
		value := scoreText(finding)
		bar := networkMissingValue
		if finding.Score != nil {
			bar = scoreBar(*finding.Score)
		}
		table.Rows = append(table.Rows, []model.Value{
			model.KeyValue(networkSourceNameKey(id)),
			scoreTextValue(finding, value),
			networkRiskValue(finding.Risk),
			scoreBarValue(finding, bar),
			networkScoreKindValue(finding.ScoreKind),
			model.KeyValue(scoreBands(id)),
			findingAccessValue(finding),
		})
	}
	return table
}

func scoreBands(id string) string {
	return networkScoreBandKey(id)
}

func (bundle ipQualityBundle) factorTable() model.Table {
	columns := []model.TableColumn{{Key: "factor", Label: "probe.network.column.factor"}}
	for _, id := range factorSourceOrder {
		columns = append(columns, model.TableColumn{Key: id, Label: networkSourceNameKey(id)})
	}
	table := model.Table{
		Key:         ipQualityTableKey(bundle.Version, "factors"),
		Title:       "probe.network.table.ipquality.factors",
		Columns:     columns,
		RowIdentity: "factor",
	}
	type factor struct {
		label string
		value func(qualityFinding) string
	}
	factors := []factor{
		{"probe.network.factor.country", func(f qualityFinding) string { return factorCountry(f) }},
		{"probe.network.factor.proxy", func(f qualityFinding) string { return factorSignal(f, f.Proxy) }},
		{"probe.network.factor.tor", func(f qualityFinding) string { return factorSignal(f, f.Tor) }},
		{"probe.network.factor.vpn", func(f qualityFinding) string { return factorSignal(f, f.VPN) }},
		{"probe.network.factor.datacenter", func(f qualityFinding) string { return factorSignal(f, f.Server) }},
		{"probe.network.factor.abuse", func(f qualityFinding) string { return factorSignal(f, f.Abuser) }},
		{"probe.network.factor.robot", func(f qualityFinding) string { return factorSignal(f, f.Robot) }},
	}
	for _, item := range factors {
		row := []model.Value{model.KeyValue(item.label)}
		for _, id := range factorSourceOrder {
			value := item.value(bundle.Findings[id])
			if item.label == "probe.network.factor.country" {
				row = append(row, factorCountryValue(bundle.Findings[id], value))
			} else {
				row = append(row, factorSignalValue(value))
			}
		}
		table.Rows = append(table.Rows, row)
	}
	return table
}

func (bundle ipQualityBundle) statusTable() model.Table {
	table := model.Table{
		Key:   ipQualityTableKey(bundle.Version, "sources"),
		Title: "probe.network.table.ipquality.sources",
		Columns: []model.TableColumn{
			{Key: "source", Label: "probe.network.column.source"},
			{Key: "status", Label: "probe.network.column.status"},
			{Key: "channel", Label: "probe.network.column.channel"},
			{Key: "duration_ms", Label: "probe.network.column.duration"},
		},
		RowIdentity: "source",
	}
	table.Rows = append(table.Rows, []model.Value{
		model.KeyValue(networkSourceNameKey("maxmind")),
		model.KeyValue(originStatus(bundle.Origin)),
		networkAccessValue(originAccess(bundle.Origin)),
		durationValue(bundle.Origin.Latency),
	})
	for _, id := range qualitySourceOrder {
		if id == "maxmind" {
			continue
		}
		finding := bundle.Findings[id]
		table.Rows = append(table.Rows, []model.Value{
			model.KeyValue(networkSourceNameKey(id)),
			model.KeyValue(findingStatus(finding)),
			findingAccessValue(finding),
			durationValue(finding.Latency),
		})
	}
	return table
}

func (bundle ipQualityBundle) measurements() []model.Measurement {
	var measurements []model.Measurement
	for _, id := range scoreSourceOrder {
		finding := bundle.Findings[id]
		if finding.Score == nil {
			continue
		}
		value := *finding.Score
		method := networkScoreMethodKey(id)
		label := "probe.network.metric.risk_score"
		display := formatScore(value) + "/100"
		if id == "dbip" {
			display = formatScore(value) + "*/100"
		}
		measurements = append(measurements, model.Measurement{
			Key:            "ipv" + bundle.Version + "_" + id + "_risk_score",
			Label:          label,
			Value:          value,
			Unit:           "/100",
			Display:        model.RawValue(display),
			Rating:         finding.Risk,
			Method:         method,
			HigherIsBetter: model.BoolPtr(false),
		})
	}
	return measurements
}

func (bundle ipQualityBundle) successfulSources() (successful, enabled int) {
	if bundle.Origin.Enabled {
		enabled++
		if bundle.Origin.Err == nil {
			successful++
		}
	}
	for _, finding := range bundle.Findings {
		if !finding.Enabled {
			continue
		}
		enabled++
		if finding.Err == nil {
			successful++
		}
	}
	return successful, enabled
}

func (bundle ipQualityBundle) failedSourceIDs() []string {
	var names []string
	if bundle.Origin.Enabled && bundle.Origin.Err != nil {
		names = append(names, "maxmind")
	}
	for _, id := range qualitySourceOrder {
		if id == "maxmind" {
			continue
		}
		finding := bundle.Findings[id]
		if finding.Enabled && finding.Err != nil {
			names = append(names, id)
		}
	}
	return names
}

func (bundle ipQualityBundle) partialSourceIDs() []string {
	var names []string
	for _, id := range qualitySourceOrder {
		if id == "maxmind" {
			continue
		}
		finding := bundle.Findings[id]
		if finding.Enabled && finding.Err == nil && finding.Partial != "" {
			names = append(names, id)
		}
	}
	return names
}

func (bundle ipQualityBundle) needsWarning() bool {
	return len(bundle.failedSourceIDs()) > 0 || len(bundle.partialSourceIDs()) > 0
}
