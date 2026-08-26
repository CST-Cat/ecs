package probe

import (
	"context"
	"strings"

	"ecs/internal/config"
	"ecs/internal/model"
)

type natSemanticProbe struct{}

func (natSemanticProbe) ID() string         { return "nat" }
func (natSemanticProbe) Title() string      { return "module.nat.title" }
func (natSemanticProbe) NeedsNetwork() bool { return true }

func (natSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (natProbe{}).Run(ctx, env)
	stabilizeNATResult(&result)
	if len(env.Config.STUNServers) > 0 {
		result.Tables = append(result.Tables, natSTUNPoolTable(env.Config.STUNServers))
	}
	return result
}

// natSTUNPoolTable discloses the configured candidate pool as machine rows.
// It is intentionally built from configuration rather than findings so that
// early stop, failed probes, and partial execution never hide candidates.
func natSTUNPoolTable(servers []config.Endpoint) model.Table {
	table := model.Table{
		Key:        "network.nat.stun_pool",
		Title:      "probe.nat.table.stun_pool",
		Columns:    []string{"probe.nat.column.server_name", "probe.nat.column.server_address"},
		ColumnKeys: []string{"server_name", "server_address"},
		Rows:       make([][]string, 0, len(servers)),
	}
	for _, server := range servers {
		table.Rows = append(table.Rows, []string{server.Name, server.Address})
	}
	return table
}

func stabilizeNATResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.nat.title"
	result.Description = "probe.nat.description"
	result.Methodology.Label = "methodology.protocol-measurement"
	result.Methodology.Profile = "probe.nat.profile"
	result.Methodology.ComparisonScope = "probe.nat.comparison_scope"
	for index := range result.Fields {
		field := &result.Fields[index]
		field.Label = "probe.nat.field." + field.Key
		switch field.Key {
		case "nat_category":
			field.Value = natCategoryKey(field.Value)
		case "mapping_behaviour":
			field.Value = natBehaviorKey(field.Value, false)
		case "filtering_behaviour":
			field.Value = natBehaviorKey(field.Value, true)
		case "behind_nat":
			switch field.Value {
			case "yes":
				field.Value = "probe.nat.boolean.yes"
			case "no":
				field.Value = "probe.nat.boolean.no"
			}
		}
	}
	for index := range result.Measurements {
		result.Measurements[index].Label = "probe.nat.metric." + result.Measurements[index].Key
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		if table.Key != "network.nat.stun" {
			continue
		}
		table.Title = "probe.nat.table.stun"
		table.Columns = []string{
			"probe.nat.column.protocol", "probe.nat.column.server", "probe.nat.column.mapped_address",
			"probe.nat.column.mapping", "probe.nat.column.filtering", "probe.nat.column.alternate_address", "probe.nat.column.status",
		}
		for rowIndex := range table.Rows {
			row := table.Rows[rowIndex]
			if len(row) < 7 {
				continue
			}
			row[3] = natBehaviorKey(row[3], false)
			row[4] = natBehaviorKey(row[4], true)
			switch row[6] {
			case "complete":
				row[6] = "probe.nat.status.complete"
			case "udp_blocked":
				row[6] = "probe.nat.status.udp_blocked"
			case "failed":
				row[6] = "probe.nat.status.failed"
			}
		}
	}
	for index := range result.Sources {
		result.Sources[index].Purpose = "probe.nat.source.rfc"
	}
	result.Notes = stableNATNotes(*result)
	switch {
	case result.Status == model.StatusSkipped:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.nat.summary.skipped")}
	case result.Evidence != nil && result.Evidence.Valid == 0:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.nat.summary.all_failed")}
	default:
		category := "probe.nat.summary.unknown"
		if field, ok := fieldByKey(*result, "nat_category"); ok {
			category = natSummaryKey(field.Value)
		}
		result.SummaryMessages = []model.Message{model.NewMessage(category)}
	}
}

func natBehaviorKey(value string, filtering bool) string {
	prefix := "probe.nat.mapping."
	if filtering {
		prefix = "probe.nat.filtering."
	}
	switch value {
	case mappingEndpointIndependent:
		return prefix + "endpoint_independent"
	case mappingAddressDependent:
		return prefix + "address_dependent"
	case mappingAddressPortDependent:
		return prefix + "address_port_dependent"
	case mappingUnknown:
		return prefix + "unknown"
	default:
		return value
	}
}

func natCategoryKey(value string) string {
	switch value {
	case "public":
		return "probe.nat.category.public"
	case "symmetric":
		return "probe.nat.category.symmetric"
	case "full_cone":
		return "probe.nat.category.full_cone"
	case "restricted_cone":
		return "probe.nat.category.restricted_cone"
	case "port_restricted":
		return "probe.nat.category.port_restricted"
	case "cone_unknown_filtering":
		return "probe.nat.category.cone_unknown_filtering"
	case "unknown":
		return "probe.nat.category.unknown"
	default:
		return value
	}
}

func natSummaryKey(category string) string {
	switch category {
	case "probe.nat.category.public":
		return "probe.nat.summary.public"
	case "probe.nat.category.symmetric":
		return "probe.nat.summary.symmetric"
	case "probe.nat.category.full_cone":
		return "probe.nat.summary.full_cone"
	case "probe.nat.category.restricted_cone":
		return "probe.nat.summary.restricted_cone"
	case "probe.nat.category.port_restricted":
		return "probe.nat.summary.port_restricted"
	case "probe.nat.category.cone_unknown_filtering":
		return "probe.nat.summary.cone_unknown_filtering"
	default:
		return "probe.nat.summary.unknown"
	}
}

func stableNATNotes(result model.Result) []string {
	notes := []string{"probe.nat.note.mapping", "probe.nat.note.udp_scope"}
	if result.Status == model.StatusSkipped {
		return append(notes, "probe.nat.note.skipped")
	}
	if result.Evidence != nil && result.Evidence.Valid == 0 {
		return append(notes, "probe.nat.note.no_response")
	}
	if result.Status == model.StatusWarning {
		notes = append(notes, "probe.nat.note.limited_evidence")
	}
	return notes
}

type appsSemanticProbe struct{}

func (appsSemanticProbe) ID() string         { return "apps" }
func (appsSemanticProbe) Title() string      { return "module.apps.title" }
func (appsSemanticProbe) NeedsNetwork() bool { return true }

func (appsSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (appsProbe{}).Run(ctx, env)
	stabilizeAppsResult(&result)
	return result
}

func stabilizeAppsResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.apps.title"
	result.Description = "probe.apps.description"
	result.Methodology.Label = "methodology.protocol-measurement"
	result.Methodology.Profile = "probe.apps.profile"
	result.Methodology.ComparisonScope = "probe.apps.comparison_scope"
	for index := range result.Fields {
		result.Fields[index].Label = "probe.apps.field." + result.Fields[index].Key
	}
	for index := range result.Measurements {
		result.Measurements[index].Label = "probe.apps.metric." + result.Measurements[index].Key
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		category := strings.TrimPrefix(table.Key, "network.apps.")
		table.Title = "probe.apps.table." + category
		table.Columns = []string{
			"probe.apps.column.service", "probe.apps.column.endpoint", "probe.apps.column.purpose",
			"probe.apps.column.status", "probe.apps.column.detail",
		}
		for rowIndex := range table.Rows {
			row := table.Rows[rowIndex]
			if len(row) < 4 {
				continue
			}
			switch row[3] {
			case "reachable":
				row[3] = "probe.apps.status.reachable"
			case "unreachable":
				row[3] = "probe.apps.status.unreachable"
			}
		}
	}
	result.Notes = []string{
		"probe.apps.note.handshake_only",
		"probe.apps.note.service_scope",
		"probe.apps.note.telegram_targets",
	}
	if result.Status == model.StatusSkipped {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.apps.summary.skipped")}
	} else {
		value := ""
		if len(result.Measurements) > 0 {
			value = result.Measurements[0].Display
		}
		result.SummaryMessages = []model.Message{model.NewMessage("probe.apps.summary.values", value)}
	}
}

type blacklistSemanticProbe struct{}

func (blacklistSemanticProbe) ID() string         { return "blacklist" }
func (blacklistSemanticProbe) Title() string      { return "module.blacklist.title" }
func (blacklistSemanticProbe) NeedsNetwork() bool { return true }

func (blacklistSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (blacklistProbe{}).Run(ctx, env)
	stabilizeBlacklistResult(&result)
	return result
}

func stabilizeBlacklistResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.blacklist.title"
	result.Description = "probe.blacklist.description"
	result.Methodology.Label = "methodology.protocol-measurement"
	result.Methodology.Profile = "probe.blacklist.profile"
	result.Methodology.ComparisonScope = "probe.blacklist.comparison_scope"
	legacyNotes := append([]string(nil), result.Notes...)
	for index := range result.Fields {
		field := &result.Fields[index]
		if strings.HasPrefix(field.Label, "probe.rdns.") {
			continue
		}
		field.Label = "probe.blacklist.field." + field.Key
	}
	for index := range result.Measurements {
		measurement := &result.Measurements[index]
		if measurement.Key == "fcrdns_passed" {
			continue
		}
		measurement.Label = "probe.blacklist.metric." + measurement.Key
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		if table.Key == "network.reverse_dns.checks" {
			continue
		}
		table.Title = "probe.blacklist.table.results"
		table.Columns = []string{
			"probe.blacklist.column.list", "probe.blacklist.column.outcome", "probe.blacklist.column.code",
			"probe.blacklist.column.scope", "probe.blacklist.column.latency",
		}
		for rowIndex := range table.Rows {
			row := table.Rows[rowIndex]
			if len(row) < 2 {
				continue
			}
			switch row[1] {
			case string(dnsblListed):
				row[1] = "probe.blacklist.outcome.listed"
			case string(dnsblClean):
				row[1] = "probe.blacklist.outcome.clean"
			case string(dnsblRefused):
				row[1] = "probe.blacklist.outcome.refused"
			case string(dnsblFailed):
				row[1] = "probe.blacklist.outcome.failed"
			}
		}
	}
	for index := range result.Sources {
		result.Sources[index].Purpose = "probe.blacklist.source.list"
	}
	result.Notes = []string{
		"probe.blacklist.note.query_scope",
		"probe.blacklist.note.list_semantics",
		"probe.blacklist.note.refused_code",
	}
	for _, note := range legacyNotes {
		if strings.HasPrefix(note, "probe.rdns.") {
			result.Notes = append(result.Notes, note)
		}
	}
	if result.Status == model.StatusSkipped {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.blacklist.summary.skipped")}
	} else {
		listed, clean, refused, failed := blacklistCounts(*result)
		switch {
		case listed > 0:
			result.SummaryMessages = []model.Message{model.NewMessage("probe.blacklist.summary.listed", listed, clean+listed+refused+failed)}
		case refused > 0 && clean == 0 && failed == 0:
			result.SummaryMessages = []model.Message{model.NewMessage("probe.blacklist.summary.refused")}
		default:
			result.SummaryMessages = []model.Message{model.NewMessage("probe.blacklist.summary.clean", clean)}
		}
	}
}

func blacklistCounts(result model.Result) (listed, clean, refused, failed int) {
	for _, measurement := range result.Measurements {
		switch measurement.Key {
		case "dnsbl_listed_count":
			listed = int(measurement.Value)
		case "dnsbl_clean_count":
			clean = int(measurement.Value)
		case "dnsbl_refused_count":
			refused = int(measurement.Value)
		case "dnsbl_failed_count":
			failed = int(measurement.Value)
		}
	}
	return
}

type bgpSemanticProbe struct{}

func (bgpSemanticProbe) ID() string         { return "bgp" }
func (bgpSemanticProbe) Title() string      { return "module.bgp.title" }
func (bgpSemanticProbe) NeedsNetwork() bool { return true }

func (bgpSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (bgpProbe{}).Run(ctx, env)
	stabilizeBGPResult(&result)
	return result
}

func stabilizeBGPResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.bgp.title"
	result.Description = "probe.bgp.description"
	result.Methodology.Label = "methodology.provider-assessment"
	result.Methodology.Profile = "probe.bgp.profile"
	result.Methodology.ComparisonScope = "probe.bgp.comparison_scope"
	for index := range result.Fields {
		if strings.HasPrefix(result.Fields[index].Key, "ipv") {
			result.Fields[index].Label = "probe.bgp.field.ip_family"
		} else {
			result.Fields[index].Label = "probe.bgp.field." + result.Fields[index].Key
		}
	}
	for index := range result.Measurements {
		result.Measurements[index].Label = "probe.bgp.metric." + result.Measurements[index].Key
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		if table.Key != "network.bgp.observation" {
			continue
		}
		table.Title = "probe.bgp.table.observation"
		table.Columns = []string{
			"probe.bgp.column.ip_family", "probe.bgp.column.prefix", "probe.bgp.column.origin_asn",
			"probe.bgp.column.rpki", "probe.bgp.column.peer_collector", "probe.bgp.column.as_path",
		}
	}
	for index := range result.Sources {
		result.Sources[index].Purpose = "probe.bgp.source.routeviews"
	}
	result.Notes = []string{
		"probe.bgp.note.public_observation",
		"probe.bgp.note.longest_match",
		"probe.bgp.note.as_path_scope",
		"probe.bgp.note.no_observation",
	}
	observed := 0
	if len(result.Measurements) > 0 {
		observed = int(result.Measurements[0].Value)
	}
	result.SummaryMessages = []model.Message{model.NewMessage("probe.bgp.summary.values", observed)}
}
