package contract

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DetectChanges compares two contracts and reports every breaking change.
func DetectChanges(new, old *Contract) []BreakingChange {
	if old == nil || new == nil {
		return nil
	}

	var oldSchema, newSchema map[string]interface{}
	if err := json.Unmarshal(old.Schema, &oldSchema); err != nil {
		return nil
	}
	if err := json.Unmarshal(new.Schema, &newSchema); err != nil {
		return nil
	}

	return detectChangesByFormat(new.Format, newSchema, oldSchema)
}

func detectChangesByFormat(format ContractFormat, newSchema, oldSchema map[string]interface{}) []BreakingChange {
	switch format {
	case FormatOpenAPI:
		return detectOpenAPIChanges(newSchema, oldSchema)
	case FormatProtobuf:
		return detectSchemaRemovals(newSchema, oldSchema, "services", "messages")
	case FormatGraphQL:
		return detectSchemaRemovals(newSchema, oldSchema, "types")
	}
	return nil
}

func detectOpenAPIChanges(newSchema, oldSchema map[string]interface{}) []BreakingChange {
	var changes []BreakingChange

	// Compare path (endpoint) changes.
	newPaths, _ := newSchema["paths"].(map[string]interface{})
	oldPaths, _ := oldSchema["paths"].(map[string]interface{})

	for path := range oldPaths {
		if _, ok := newPaths[path]; !ok {
			changes = append(changes, BreakingChange{
				Field:      "paths." + path,
				ChangeType: "endpoint_removed",
			})
		}
	}

	for path, newMethods := range newPaths {
		oldMethods, ok := oldPaths[path].(map[string]interface{})
		if !ok {
			continue
		}
		newM, ok := newMethods.(map[string]interface{})
		if !ok {
			continue
		}
		for method := range oldMethods {
			if _, ok := newM[method]; !ok {
				changes = append(changes, BreakingChange{
					Field:      fmt.Sprintf("paths.%s.%s", path, method),
					ChangeType: "endpoint_removed",
				})
			}
		}
	}

	return changes
}

func detectSchemaRemovals(newSchema, oldSchema map[string]interface{}, keys ...string) []BreakingChange {
	var changes []BreakingChange
	for _, key := range keys {
		newItems := schemaList(newSchema, key)
		oldItems := schemaList(oldSchema, key)
		oldByName := indexByName(oldItems)

		for _, ni := range newItems {
			name := fieldName(ni)
			oi, ok := oldByName[name]
			if !ok {
				continue
			}
			nFields := schemaList(ni, "fields")
			oFields := schemaList(oi, "fields")
			oFieldByName := indexByName(oFields)

			for _, nf := range nFields {
				nfName := fieldName(nf)
				of, ok := oFieldByName[nfName]
				if !ok {
					changes = append(changes, BreakingChange{
						Field:      fmt.Sprintf("%s.%s.%s", key, name, nfName),
						ChangeType: "field_removed",
					})
					continue
				}
				oldType := fieldStr(of, "type")
				newType := fieldStr(nf, "type")
				if oldType != "" && newType != "" && oldType != newType {
					changes = append(changes, BreakingChange{
						Field:      fmt.Sprintf("%s.%s.%s", key, name, nfName),
						OldType:    oldType,
						NewType:    newType,
						ChangeType: "type_changed",
					})
				}
			}
		}

		// Removed from old but not in new.
		for _, oi := range oldItems {
			name := fieldName(oi)
			found := false
			for _, ni := range newItems {
				if fieldName(ni) == name {
					found = true
					break
				}
			}
			if !found {
				changes = append(changes, BreakingChange{
					Field:      fmt.Sprintf("%s.%s", key, name),
					ChangeType: "endpoint_removed",
				})
			}
		}
	}
	return changes
}

// ConsumerImpactReport maps breaking changes to affected consumers.
func ConsumerImpactReport(changes []BreakingChange, consumerCatalog map[string][]string) []ConsumerImpact {
	if len(changes) == 0 || len(consumerCatalog) == 0 {
		return nil
	}

	var impacts []ConsumerImpact
	for consumerName, fields := range consumerCatalog {
		var affected []string
		for _, ch := range changes {
			if matchesConsumer(ch.Field, fields) {
				affected = append(affected, ch.Field)
			}
		}
		if len(affected) > 0 {
			impacts = append(impacts, ConsumerImpact{
				ConsumerName:    consumerName,
				BreakingChanges: affected,
				Severity:        impactSeverity(affected),
			})
		}
	}
	return impacts
}

func matchesConsumer(field string, fields []string) bool {
	for _, f := range fields {
		if f == "*" || f == field || strings.HasPrefix(field, f) {
			return true
		}
	}
	return false
}

func impactSeverity(fields []string) string {
	switch {
	case len(fields) >= 5:
		return "critical"
	case len(fields) >= 3:
		return "high"
	case len(fields) >= 2:
		return "medium"
	default:
		return "low"
	}
}

// --- helpers ---

func schemaList(schema map[string]interface{}, key string) []map[string]interface{} {
	raw, ok := schema[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var out []map[string]interface{}
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func indexByName(items []map[string]interface{}) map[string]map[string]interface{} {
	out := make(map[string]map[string]interface{})
	for _, m := range items {
		out[fieldName(m)] = m
	}
	return out
}

func fieldName(m map[string]interface{}) string {
	if n, ok := m["name"].(string); ok {
		return n
	}
	return ""
}

func fieldStr(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
