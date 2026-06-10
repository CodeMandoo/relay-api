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

var errModelGroupDeleted = errors.New("当前分组已删除")

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
	if err := a.db.Select("id").First(&group, groupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, errModelGroupDeleted
		}
		return 0, err
	}
	return group.ID, nil
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
		Name        string                `json:"name"`
		Description string                `json:"description"`
		Bindings    []modelBindingRequest `json:"bindings"`
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
		Name:         name,
		Description:  strings.TrimSpace(req.Description),
		BindingsJSON: encodeModelGroupBindings(bindings),
	}
	if err := a.db.Create(&group).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "create model group failed")
		return
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
	if bindingRequests, hasBindingRequests := parseBindingRequests(req["bindings"]); hasBindingRequests {
		if len(bindingRequests) == 0 {
			updates["bindings_json"] = ""
		} else {
			parsed, err := a.validateModelBindingRequests(bindingRequests)
			if err != nil {
				errorJSON(c, http.StatusBadRequest, err.Error())
				return
			}
			updates["bindings_json"] = encodeModelGroupBindings(parsed)
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
	var modelCount int64
	if err := a.db.Model(&ModelConfig{}).Where("model_group_id = ?", group.ID).Count(&modelCount).Error; err != nil {
		errorJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	if modelCount > 0 {
		errorJSON(c, http.StatusBadRequest, "model group is not empty")
		return
	}
	if err := a.db.Delete(&group).Error; err != nil {
		errorJSON(c, http.StatusBadRequest, "delete model group failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *App) userModelGroups(c *gin.Context) {
	if _, ok := currentUser(c); !ok {
		errorJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var groups []ModelGroup
	if err := a.db.Order("is_default desc, created_at asc, id asc").Find(&groups).Error; err != nil {
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
