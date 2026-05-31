package app

import (
	"errors"
	"sort"

	"gorm.io/gorm"
)

type modelBindingRequest struct {
	ID             string `json:"id"`
	SourceID       string `json:"sourceId"`
	SourceKeyID    string `json:"sourceKeyId"`
	RoutingWeight  int    `json:"routingWeight"`
	RoutingEnabled *bool  `json:"routingEnabled"`
	Enabled        *bool  `json:"enabled"`
}

func migrateModelRouteBindings(db *gorm.DB) error {
	var models []ModelConfig
	if err := db.Order("name asc, id asc").Find(&models).Error; err != nil {
		return err
	}
	if len(models) == 0 {
		return nil
	}
	var sources []UpstreamSource
	if err := db.Find(&sources).Error; err != nil {
		return err
	}
	sourceMap := map[uint]UpstreamSource{}
	for _, source := range sources {
		sourceMap[source.ID] = source
	}
	groups := map[string][]ModelConfig{}
	for _, model := range models {
		groups[model.Name] = append(groups[model.Name], model)
	}
	for _, group := range groups {
		canonical := preferredModelConfig(group, sourceMap)
		if err := ensureBindingsForLegacyModels(db, canonical, group); err != nil {
			return err
		}
		if len(group) <= 1 {
			continue
		}
		if err := mergeLegacyModelGroup(db, canonical, group); err != nil {
			return err
		}
	}
	return nil
}

func preferredModelConfig(models []ModelConfig, sources map[uint]UpstreamSource) ModelConfig {
	sort.SliceStable(models, func(i, j int) bool {
		leftActive := models[i].Status == ModelStatusActive
		rightActive := models[j].Status == ModelStatusActive
		if leftActive != rightActive {
			return leftActive
		}
		leftSource := sources[models[i].SourceID]
		rightSource := sources[models[j].SourceID]
		if leftSource.Priority != rightSource.Priority {
			return leftSource.Priority < rightSource.Priority
		}
		leftWeight := nonZeroInt(models[i].RoutingWeight, 1)
		rightWeight := nonZeroInt(models[j].RoutingWeight, 1)
		if leftWeight != rightWeight {
			return leftWeight > rightWeight
		}
		return models[i].ID < models[j].ID
	})
	return models[0]
}

func ensureBindingsForLegacyModels(db *gorm.DB, canonical ModelConfig, models []ModelConfig) error {
	for _, model := range models {
		if model.SourceID == 0 {
			continue
		}
		var count int64
		query := db.Model(&ModelRouteBinding{}).Where("model_id = ? AND source_id = ?", canonical.ID, model.SourceID)
		if model.SourceKeyID == nil {
			query = query.Where("source_key_id IS NULL")
		} else {
			query = query.Where("source_key_id = ?", *model.SourceKeyID)
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		binding := ModelRouteBinding{
			ModelID:        canonical.ID,
			SourceID:       model.SourceID,
			SourceKeyID:    model.SourceKeyID,
			RoutingWeight:  nonZeroInt(model.RoutingWeight, 1),
			RoutingEnabled: model.RoutingEnabled,
			Enabled:        model.Status == ModelStatusActive,
			LatencyMS:      model.LatencyMS,
		}
		if err := db.Create(&binding).Error; err != nil {
			return err
		}
	}
	return nil
}

func mergeLegacyModelGroup(db *gorm.DB, canonical ModelConfig, group []ModelConfig) error {
	enabled := false
	formats := map[string]bool{}
	var duplicateIDs []uint
	for _, model := range group {
		enabled = enabled || model.Status == ModelStatusActive
		for _, format := range modelFormatList(model) {
			formats[format] = true
		}
		if model.ID != canonical.ID {
			duplicateIDs = append(duplicateIDs, model.ID)
		}
	}
	status := ModelStatusDisabled
	if enabled {
		status = ModelStatusActive
	}
	formatList := make([]string, 0, len(formats))
	for format := range formats {
		formatList = append(formatList, format)
	}
	sort.Strings(formatList)
	updates := map[string]any{
		"source_id":         canonical.SourceID,
		"source_key_id":     canonical.SourceKeyID,
		"provider":          canonical.Provider,
		"formats":           normalizeModelFormats(formatList, canonical.Provider),
		"status":            status,
		"routing_weight":    nonZeroInt(canonical.RoutingWeight, 1),
		"routing_enabled":   canonical.RoutingEnabled,
		"latency_ms":        canonical.LatencyMS,
		"input_price":       canonical.InputPrice,
		"output_price":      canonical.OutputPrice,
		"cache_write_price": canonical.CacheWritePrice,
		"cache_read_price":  canonical.CacheReadPrice,
	}
	if err := db.Model(&ModelConfig{}).Where("id = ?", canonical.ID).Updates(updates).Error; err != nil {
		return err
	}
	if len(duplicateIDs) > 0 {
		if err := db.Delete(&ModelConfig{}, duplicateIDs).Error; err != nil {
			return err
		}
	}
	return nil
}

func legacyBindingFromModel(model ModelConfig) ModelRouteBinding {
	return ModelRouteBinding{
		ModelID:        model.ID,
		SourceID:       model.SourceID,
		SourceKeyID:    model.SourceKeyID,
		RoutingWeight:  nonZeroInt(model.RoutingWeight, 1),
		RoutingEnabled: model.RoutingEnabled,
		Enabled:        model.Status == ModelStatusActive,
		LatencyMS:      model.LatencyMS,
	}
}

func (a *App) modelBindings(model ModelConfig) ([]ModelRouteBinding, error) {
	var bindings []ModelRouteBinding
	if err := a.db.Where("model_id = ?", model.ID).Order("id asc").Find(&bindings).Error; err != nil {
		return nil, err
	}
	if len(bindings) == 0 && model.SourceID != 0 {
		bindings = append(bindings, legacyBindingFromModel(model))
	}
	return bindings, nil
}

func sourceKeyIDValueFromBinding(binding ModelRouteBinding) uint {
	if binding.SourceKeyID == nil {
		return 0
	}
	return *binding.SourceKeyID
}

func parseBindingRequests(raw any) ([]modelBindingRequest, bool) {
	rows, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	out := make([]modelBindingRequest, 0, len(rows))
	for _, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		req := modelBindingRequest{}
		if value, ok := row["id"].(string); ok {
			req.ID = value
		}
		if value, ok := row["sourceId"].(string); ok {
			req.SourceID = value
		}
		if value, ok := row["sourceKeyId"].(string); ok {
			req.SourceKeyID = value
		}
		if value, ok := numberFromMap(row, "routingWeight"); ok {
			req.RoutingWeight = int(value)
		}
		if value, ok := row["routingEnabled"].(bool); ok {
			req.RoutingEnabled = &value
		}
		if value, ok := row["enabled"].(bool); ok {
			req.Enabled = &value
		}
		out = append(out, req)
	}
	return out, true
}

func normalizeBindingRequests(bindings []modelBindingRequest, fallback modelBindingRequest) []modelBindingRequest {
	if len(bindings) == 0 {
		return []modelBindingRequest{fallback}
	}
	return bindings
}

func (a *App) validateModelBindingRequests(bindings []modelBindingRequest) ([]modelBindingRequest, error) {
	out := make([]modelBindingRequest, 0, len(bindings))
	seen := map[uint]bool{}
	for _, binding := range bindings {
		sourceID, err := parseNumericID(binding.SourceID)
		if err != nil {
			return nil, err
		}
		if seen[sourceID] {
			return nil, errors.New("duplicate source binding")
		}
		seen[sourceID] = true
		if _, err := a.getSourceForModel(sourceID); err != nil {
			return nil, err
		}
		sourceKeyID, err := a.resolveSourceKeyID(sourceID, binding.SourceKeyID)
		if err != nil {
			return nil, err
		}
		weight := binding.RoutingWeight
		if weight <= 0 {
			weight = 1
		}
		routingEnabled := true
		if binding.RoutingEnabled != nil {
			routingEnabled = *binding.RoutingEnabled
		}
		enabled := true
		if binding.Enabled != nil {
			enabled = *binding.Enabled
		}
		normalized := modelBindingRequest{
			ID:             binding.ID,
			SourceID:       id("s", sourceID),
			RoutingWeight:  weight,
			RoutingEnabled: &routingEnabled,
			Enabled:        &enabled,
		}
		if sourceKeyID != nil {
			normalized.SourceKeyID = id("sk", *sourceKeyID)
		} else {
			normalized.SourceKeyID = "default"
		}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil, errors.New("sourceId is required")
	}
	return out, nil
}

func (a *App) replaceModelBindings(modelID uint, bindings []modelBindingRequest) error {
	keep := make([]uint, 0, len(bindings))
	for _, binding := range bindings {
		sourceID, err := parseNumericID(binding.SourceID)
		if err != nil {
			return err
		}
		var sourceKeyID *uint
		if binding.SourceKeyID != "" && binding.SourceKeyID != "default" {
			parsed, err := parseNumericID(binding.SourceKeyID)
			if err != nil {
				return err
			}
			sourceKeyID = &parsed
		}
		routingEnabled := true
		if binding.RoutingEnabled != nil {
			routingEnabled = *binding.RoutingEnabled
		}
		enabled := true
		if binding.Enabled != nil {
			enabled = *binding.Enabled
		}
		weight := binding.RoutingWeight
		if weight <= 0 {
			weight = 1
		}
		if binding.ID != "" {
			bindingID, err := parseNumericID(binding.ID)
			if err != nil {
				return err
			}
			var sourceKeyValue any = gorm.Expr("NULL")
			if sourceKeyID != nil {
				sourceKeyValue = *sourceKeyID
			}
			updates := map[string]any{
				"source_id":       sourceID,
				"source_key_id":   sourceKeyValue,
				"routing_weight":  weight,
				"routing_enabled": routingEnabled,
				"enabled":         enabled,
			}
			result := a.db.Model(&ModelRouteBinding{}).Where("id = ? AND model_id = ?", bindingID, modelID).Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				keep = append(keep, bindingID)
				continue
			}
		}
		row := ModelRouteBinding{
			ModelID:        modelID,
			SourceID:       sourceID,
			SourceKeyID:    sourceKeyID,
			RoutingWeight:  weight,
			RoutingEnabled: routingEnabled,
			Enabled:        enabled,
		}
		if err := a.db.Create(&row).Error; err != nil {
			return err
		}
		keep = append(keep, row.ID)
	}
	if len(keep) == 0 {
		return errors.New("sourceId is required")
	}
	return a.db.Where("model_id = ? AND id NOT IN ?", modelID, keep).Delete(&ModelRouteBinding{}).Error
}

func (a *App) syncModelLegacyBindingFields(modelID uint, binding modelBindingRequest) error {
	sourceID, err := parseNumericID(binding.SourceID)
	if err != nil {
		return err
	}
	var sourceKeyID any = gorm.Expr("NULL")
	if binding.SourceKeyID != "" && binding.SourceKeyID != "default" {
		parsed, err := parseNumericID(binding.SourceKeyID)
		if err != nil {
			return err
		}
		sourceKeyID = parsed
	}
	routingEnabled := true
	if binding.RoutingEnabled != nil {
		routingEnabled = *binding.RoutingEnabled
	}
	weight := binding.RoutingWeight
	if weight <= 0 {
		weight = 1
	}
	return a.db.Model(&ModelConfig{}).Where("id = ?", modelID).Updates(map[string]any{
		"source_id":       sourceID,
		"source_key_id":   sourceKeyID,
		"routing_weight":  weight,
		"routing_enabled": routingEnabled,
	}).Error
}

func publicIDNumber(raw string) uint {
	value, err := parseNumericID(raw)
	if err != nil {
		return 0
	}
	return value
}
