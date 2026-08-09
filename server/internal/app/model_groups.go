package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	ModelGroupStatusActive   = "active"
	ModelGroupStatusDisabled = "disabled"
)

var errModelGroupDeleted = errors.New("当前分组不存在")

func encodeModelGroupBindings(bindings []modelBindingRequest) string {
	if len(bindings) == 0 {
		return ""
	}
	raw, err := json.Marshal(bindings)
	if err != nil {
		return ""
	}
	return string(raw)
}

func decodeModelGroupBindings(raw string) []modelBindingRequest {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var bindings []modelBindingRequest
	if err := json.Unmarshal([]byte(raw), &bindings); err != nil {
		return nil
	}
	return bindings
}

func defaultPlatformModelGroup(db *gorm.DB) (ModelGroup, error) {
	if err := ensureDefaultModelGroup(db); err != nil {
		return ModelGroup{}, err
	}
	var group ModelGroup
	if err := db.Where("is_default = ?", true).Order("id asc").First(&group).Error; err != nil {
		return ModelGroup{}, err
	}
	return group, nil
}

func (a *App) defaultModelGroupID() uint {
	group, err := defaultPlatformModelGroup(a.db)
	if err != nil {
		return 0
	}
	return group.ID
}

func (a *App) normalizeModelGroupID(groupID uint) uint {
	if groupID > 0 {
		return groupID
	}
	return a.defaultModelGroupID()
}

func modelGroupBucketID(model ModelConfig, defaultGroupID uint) uint {
	if model.ModelGroupID > 0 {
		return model.ModelGroupID
	}
	return defaultGroupID
}

func modelGroupBucketKey(name string, groupID uint) string {
	return fmt.Sprintf("%d:%s", groupID, strings.TrimSpace(name))
}

func (a *App) applyModelGroupFilter(query *gorm.DB, groupID uint) *gorm.DB {
	defaultGroupID := a.defaultModelGroupID()
	if groupID == 0 || groupID == defaultGroupID {
		return query.Where("model_group_id = ? OR model_group_id = 0", defaultGroupID)
	}
	return query.Where("model_group_id = ?", groupID)
}

func (a *App) modelGroupNameMap() map[uint]string {
	var groups []ModelGroup
	a.db.Find(&groups)
	out := map[uint]string{}
	defaultID := uint(0)
	for _, group := range groups {
		out[group.ID] = group.Name
		if group.IsDefault {
			defaultID = group.ID
		}
	}
	if defaultID > 0 {
		out[0] = out[defaultID]
	}
	return out
}

func (a *App) platformModelGroupFromRequest(raw string) (ModelGroup, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultPlatformModelGroup(a.db)
	}
	groupID, err := parseNumericID(raw)
	if err != nil {
		return ModelGroup{}, fmt.Errorf("invalid modelGroupId")
	}
	var group ModelGroup
	if err := a.db.First(&group, groupID).Error; err != nil {
		return ModelGroup{}, fmt.Errorf("model group not found")
	}
	return group, nil
}

func (a *App) accessibleModelGroupForUser(_ User, raw string) (ModelGroup, error) {
	return a.platformModelGroupFromRequest(raw)
}

func (a *App) modelGroupIDForAPIKey(key APIKey) (uint, error) {
	groupID := a.normalizeModelGroupID(key.ModelGroupID)
	if groupID == 0 {
		return 0, errModelGroupDeleted
	}
	var group ModelGroup
	if err := a.db.Select("id", "status").First(&group, groupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, errModelGroupDeleted
		}
		return 0, err
	}
	if group.Status == ModelGroupStatusDisabled {
		return 0, errModelGroupDeleted
	}
	return group.ID, nil
}

func (a *App) modelGroupForRouting(groupID uint) (ModelGroup, error) {
	groupID = a.normalizeModelGroupID(groupID)
	var group ModelGroup
	if err := a.db.First(&group, groupID).Error; err != nil {
		return ModelGroup{}, err
	}
	return group, nil
}

func (a *App) fixedRouteTarget(targets []routeTarget, group ModelGroup) (routeTarget, error) {
	if group.FixedSourceID == nil || *group.FixedSourceID == 0 {
		return routeTarget{}, errors.New("fixed model is not configured")
	}
	for _, target := range targets {
		if target.Source.ID != *group.FixedSourceID {
			continue
		}
		if group.FixedSourceKeyID == nil {
			if target.Binding.SourceKeyID != nil {
				continue
			}
		} else if target.Binding.SourceKeyID == nil || *target.Binding.SourceKeyID != *group.FixedSourceKeyID {
			continue
		}
		return target, nil
	}
	return routeTarget{}, errors.New("fixed model is unavailable")
}

func (a *App) normalizeFixedRoute(group *ModelGroup, bindings []modelBindingRequest) error {
	if group.DynamicRouting || group.FixedSourceID != nil {
		return nil
	}
	var best *UpstreamSource
	var bestKey *uint
	for _, binding := range bindings {
		sourceID, err := parseNumericID(binding.SourceID)
		if err != nil {
			return err
		}
		var source UpstreamSource
		if err := a.db.First(&source, sourceID).Error; err != nil {
			return err
		}
		if best == nil || source.Priority < best.Priority || (source.Priority == best.Priority && source.ID < best.ID) {
			best = &source
			if binding.SourceKeyID != "" && binding.SourceKeyID != "default" {
				keyID, err := parseNumericID(binding.SourceKeyID)
				if err != nil {
					return err
				}
				bestKey = &keyID
			} else {
				bestKey = nil
			}
		}
	}
	if best == nil {
		return errors.New("fixed model is required when dynamic routing is disabled")
	}
	group.FixedSourceID = &best.ID
	group.FixedSourceKeyID = bestKey
	return nil
}

func (a *App) setFixedRoute(group *ModelGroup, rawSourceID, rawSourceKeyID string) error {
	if strings.TrimSpace(rawSourceID) == "" {
		group.FixedSourceID = nil
		group.FixedSourceKeyID = nil
		return nil
	}
	sourceID, err := parseNumericID(rawSourceID)
	if err != nil {
		return errors.New("invalid fixedSourceId")
	}
	if _, err := a.getSourceForModel(sourceID); err != nil {
		return err
	}
	sourceKeyID, err := a.resolveSourceKeyID(sourceID, rawSourceKeyID)
	if err != nil {
		return err
	}
	group.FixedSourceID = &sourceID
	group.FixedSourceKeyID = sourceKeyID
	return nil
}

func (a *App) adminModelGroups(c *gin.Context) {
	var groups []ModelGroup
	if err := a.db.Order("is_default desc, created_at asc, id asc").Find(&groups).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]ModelGroupDTO, 0, len(groups))
	for _, group := range groups {
		var keyCount int64
		var modelCount int64
		a.db.Model(&APIKey{}).Where("model_group_id = ?", group.ID).Count(&keyCount)
		a.db.Model(&ModelConfig{}).Where("model_group_id = ?", group.ID).Count(&modelCount)
		out = append(out, modelGroupDTO(group, keyCount, modelCount))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (a *App) adminCreateModelGroup(c *gin.Context) {
	var req struct {
		Name           string                `json:"name"`
		Description    string                `json:"description"`
		DynamicRouting *bool                 `json:"dynamicRouting"`
		FixedSourceID  string                `json:"fixedSourceId"`
		FixedSourceKey string                `json:"fixedSourceKeyId"`
		Bindings       []modelBindingRequest `json:"bindings"`
	}
	if !bindJSON(c, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		errorJSON(c, http.StatusBadRequest, "name is required")
		return
	}
	var bindings []modelBindingRequest
	if len(req.Bindings) > 0 {
		parsed, err := a.validateModelBindingRequests(req.Bindings)
		if err != nil {
			errorJSON(c, http.StatusBadRequest, err.Error())
			return
		}
		bindings = parsed
	}
	group := ModelGroup{
		Name:           name,
		Description:    strings.TrimSpace(req.Description),
		BindingsJSON:   encodeModelGroupBindings(bindings),
		DynamicRouting: req.DynamicRouting == nil || *req.DynamicRouting,
	}
	if strings.TrimSpace(req.FixedSourceID) != "" {
		if err := a.setFixedRoute(&group, req.FixedSourceID, req.FixedSourceKey); err != nil {
			errorJSON(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := a.normalizeFixedRoute(&group, bindings); err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.db.Create(&group).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "create model group failed")
		return
	}
	if !group.DynamicRouting {
		if err := a.db.Model(&group).Update("dynamic_routing", false).Error; err != nil {
			errorJSON(c, http.StatusBadRequest, "create model group failed")
			return
		}
	}
	c.JSON(http.StatusCreated, gin.H{"data": modelGroupDTO(group, 0, 0)})
}

func (a *App) adminUpdateModelGroup(c *gin.Context) {
	groupID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var group ModelGroup
	if err := a.db.First(&group, groupID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "model group not found")
		return
	}
	var req map[string]any
	if !bindJSON(c, &req) {
		return
	}
	updates := map[string]any{}
	if value, ok := req["name"].(string); ok && strings.TrimSpace(value) != "" {
		updates["name"] = strings.TrimSpace(value)
	}
	if value, ok := req["description"].(string); ok {
		updates["description"] = strings.TrimSpace(value)
	}
	bindings := decodeModelGroupBindings(group.BindingsJSON)
	if bindingRequests, hasBindingRequests := parseBindingRequests(req["bindings"]); hasBindingRequests {
		if len(bindingRequests) == 0 {
			updates["bindings_json"] = ""
			bindings = nil
		} else {
			parsed, err := a.validateModelBindingRequests(bindingRequests)
			if err != nil {
				errorJSON(c, http.StatusBadRequest, err.Error())
				return
			}
			updates["bindings_json"] = encodeModelGroupBindings(parsed)
			bindings = parsed
		}
	}
	if value, ok := req["dynamicRouting"].(bool); ok {
		group.DynamicRouting = value
		updates["dynamic_routing"] = value
	}
	if value, ok := req["status"].(string); ok && (value == ModelGroupStatusActive || value == ModelGroupStatusDisabled) {
		if group.IsDefault && value == ModelGroupStatusDisabled {
			errorJSON(c, http.StatusBadRequest, "default model group cannot be disabled")
			return
		}
		updates["status"] = value
	}
	if value, ok := req["fixedSourceId"].(string); ok {
		rawKey, _ := req["fixedSourceKeyId"].(string)
		if err := a.setFixedRoute(&group, value, rawKey); err != nil {
			errorJSON(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := a.normalizeFixedRoute(&group, bindings); err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	if group.FixedSourceID == nil {
		updates["fixed_source_id"] = gorm.Expr("NULL")
		updates["fixed_source_key_id"] = gorm.Expr("NULL")
	} else {
		updates["fixed_source_id"] = *group.FixedSourceID
		if group.FixedSourceKeyID == nil {
			updates["fixed_source_key_id"] = gorm.Expr("NULL")
		} else {
			updates["fixed_source_key_id"] = *group.FixedSourceKeyID
		}
	}
	if len(updates) == 0 {
		errorJSON(c, http.StatusBadRequest, "no fields to update")
		return
	}
	if err := a.db.Model(&group).Updates(updates).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "update model group failed")
		return
	}
	_ = a.db.First(&group, group.ID).Error
	c.JSON(http.StatusOK, gin.H{"data": modelGroupDTO(group, 0, 0)})
}

func (a *App) adminDeleteModelGroup(c *gin.Context) {
	groupID, err := parseNumericID(c.Param("id"))
	if err != nil {
		errorJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	var group ModelGroup
	if err := a.db.First(&group, groupID).Error; err != nil {
		errorJSON(c, http.StatusNotFound, "model group not found")
		return
	}
	if group.IsDefault {
		errorJSON(c, http.StatusBadRequest, "default model group cannot be deleted")
		return
	}
	// 同步删除该分组下的模型配置及路由绑定。
	var modelIDs []uint
	if err := a.db.Model(&ModelConfig{}).Where("model_group_id = ?", group.ID).Pluck("id", &modelIDs).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	if len(modelIDs) > 0 {
		_ = a.db.Where("model_id IN ?", modelIDs).Delete(&ModelRouteBinding{}).Error
		_ = a.db.Where("model_group_id = ?", group.ID).Delete(&ModelConfig{}).Error
	}
	if err := a.db.Delete(&group).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "delete model group failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "deletedModels": len(modelIDs)})
}

func (a *App) userModelGroups(c *gin.Context) {
	if _, ok := currentUser(c); !ok {
		errorJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var groups []ModelGroup
	// 用户端只展示启用状态的分组
	if err := a.db.Where("status = ?", ModelGroupStatusActive).Order("is_default desc, created_at asc, id asc").Find(&groups).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]ModelGroupDTO, 0, len(groups))
	for _, group := range groups {
		var modelCount int64
		a.db.Model(&ModelConfig{}).Where("model_group_id = ?", group.ID).Count(&modelCount)
		out = append(out, modelGroupDTO(group, 0, modelCount))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}
