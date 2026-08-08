package app

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	schedulerStateClosed     = "closed"
	schedulerStateOpen       = "open"
	schedulerStateHalfOpen   = "half_open"
	schedulerStateRecovering = "recovering"
	schedulerStateObserving  = "observing"

	schedulerFailureThreshold         = 3
	schedulerProbeInterval            = 5 * time.Minute
	schedulerRecoveryProbeInterval    = 10 * time.Second
	schedulerProbeTimeout             = 10 * time.Second
	schedulerProbeLeaseDuration       = 30 * time.Second
	schedulerObservationDuration      = 30 * time.Second
	schedulerObservingWeightPercent   = 10
	schedulerRecoverySuccessThreshold = 3

	// maxProbeLogsPerModel 每个模型保留的探测历史条数上限。
	maxProbeLogsPerModel = 50
)

func schedulerBindingState(binding ModelRouteBinding) string {
	switch binding.SchedulerState {
	case schedulerStateOpen, schedulerStateHalfOpen, schedulerStateRecovering, schedulerStateObserving:
		return binding.SchedulerState
	default:
		return schedulerStateClosed
	}
}

// anyCandidateFrozen 报告候选集中是否存在处于冻结状态的上游绑定或源级冷却。
// 用于区分“全部上游源已冻结”与“没有可用的在线上游源”。
func anyCandidateFrozen(candidates []routeTarget, now time.Time) bool {
	for _, target := range candidates {
		switch schedulerBindingState(target.Binding) {
		case schedulerStateOpen, schedulerStateHalfOpen, schedulerStateRecovering:
			return true
		}
		if target.Source.CooldownUntil != nil && target.Source.CooldownUntil.After(now) {
			return true
		}
	}
	return false
}

func schedulerResetUpdates() map[string]any {
	return map[string]any{
		"scheduler_state":   schedulerStateClosed,
		"failure_count":     0,
		"success_streak":    0,
		"cooldown_until":    nil,
		"observation_until": nil,
		"probe_lease_until": nil,
		"last_failure_at":   nil,
	}
}

func (a *App) resetSchedulerMemory() {
	a.schedulerMu.Lock()
	defer a.schedulerMu.Unlock()
	a.schedulerCurrent = map[string]int{}
}

func (a *App) resetSchedulerBindingMemory(bindingID uint) {
	if bindingID == 0 {
		return
	}
	a.schedulerMu.Lock()
	defer a.schedulerMu.Unlock()
	if a.schedulerCurrent == nil {
		a.schedulerCurrent = map[string]int{}
		return
	}
	delete(a.schedulerCurrent, fmt.Sprintf("mb:%d", bindingID))
}

func (a *App) recoverSourceBindings(sourceID uint) error {
	if sourceID == 0 {
		return nil
	}
	if err := a.db.Model(&ModelRouteBinding{}).Where("source_id = ?", sourceID).Updates(schedulerResetUpdates()).Error; err != nil {
		return err
	}
	a.resetSchedulerMemory()
	return nil
}

func (a *App) refreshSchedulerState(binding ModelRouteBinding, now time.Time) ModelRouteBinding {
	state := schedulerBindingState(binding)
	binding.SchedulerState = state
	if binding.ID == 0 || state != schedulerStateObserving || binding.ObservationUntil == nil || binding.ObservationUntil.After(now) {
		return binding
	}
	updates := schedulerResetUpdates()
	updates["last_success_at"] = now
	if err := a.db.Model(&ModelRouteBinding{}).Where("id = ? AND scheduler_state = ?", binding.ID, schedulerStateObserving).Updates(updates).Error; err == nil {
		binding.SchedulerState = schedulerStateClosed
		binding.FailureCount = 0
		binding.SuccessStreak = 0
		binding.CooldownUntil = nil
		binding.ObservationUntil = nil
	}
	return binding
}

func effectiveRoutingWeight(target routeTarget, now time.Time) int {
	if !target.Binding.Enabled {
		return 0
	}
	state := schedulerBindingState(target.Binding)
	if state == schedulerStateOpen || state == schedulerStateHalfOpen || state == schedulerStateRecovering {
		return 0
	}
	base := nonZeroInt(target.Binding.RoutingWeight, 1)
	switch state {
	case schedulerStateObserving:
		weight := base * schedulerObservingWeightPercent / 100
		return nonZeroInt(weight, 1)
	default:
		return base
	}
}

func (a *App) scheduleTargets(targets []routeTarget, now time.Time) []routeTarget {
	eligible := make([]routeTarget, 0, len(targets))
	for _, target := range targets {
		if effectiveRoutingWeight(target, now) <= 0 {
			continue
		}
		eligible = append(eligible, target)
	}
	sort.SliceStable(eligible, func(i, j int) bool { return routeTargetLess(eligible[i], eligible[j], now) })
	groups := make([][]routeTarget, 0)
	for start := 0; start < len(eligible); {
		end := start + 1
		for end < len(eligible) && eligible[end].Source.Priority == eligible[start].Source.Priority {
			end++
		}
		groups = append(groups, a.schedulePriorityGroup(eligible[start:end], now))
		start = end
	}
	if len(groups) > 1 && priorityGroupOnlyObserving(groups[0]) && !a.shouldRouteObservation(groups[0]) {
		groups[0], groups[1] = groups[1], groups[0]
	}
	ordered := make([]routeTarget, 0, len(eligible))
	for _, group := range groups {
		ordered = append(ordered, group...)
	}
	return ordered
}

func priorityGroupOnlyObserving(group []routeTarget) bool {
	if len(group) == 0 {
		return false
	}
	for _, target := range group {
		if schedulerBindingState(target.Binding) != schedulerStateObserving {
			return false
		}
	}
	return true
}

func (a *App) shouldRouteObservation(group []routeTarget) bool {
	key := fmt.Sprintf("observe:p:%d", group[0].Source.Priority)
	a.schedulerMu.Lock()
	defer a.schedulerMu.Unlock()
	count := a.schedulerCurrent[key] + 1
	a.schedulerCurrent[key] = count % 10
	return count%10 == 0
}

func (a *App) schedulePriorityGroup(group []routeTarget, now time.Time) []routeTarget {
	if len(group) <= 1 {
		return append([]routeTarget(nil), group...)
	}
	totalWeight := 0
	for _, target := range group {
		totalWeight += effectiveRoutingWeight(target, now)
	}
	a.schedulerMu.Lock()
	if a.schedulerCurrent == nil {
		a.schedulerCurrent = map[string]int{}
	}
	selected := 0
	bestCurrent := 0
	for i, target := range group {
		key := schedulerKey(target)
		next := a.schedulerCurrent[key] + effectiveRoutingWeight(target, now)
		a.schedulerCurrent[key] = next
		if i == 0 || next > bestCurrent || (next == bestCurrent && routeTargetLess(target, group[selected], now)) {
			selected = i
			bestCurrent = next
		}
	}
	a.schedulerCurrent[schedulerKey(group[selected])] -= totalWeight
	a.schedulerMu.Unlock()

	ordered := make([]routeTarget, 0, len(group))
	ordered = append(ordered, group[selected])
	remaining := make([]routeTarget, 0, len(group)-1)
	for i, target := range group {
		if i == selected {
			continue
		}
		remaining = append(remaining, target)
	}
	sort.SliceStable(remaining, func(i, j int) bool {
		return routeTargetLess(remaining[i], remaining[j], now)
	})
	return append(ordered, remaining...)
}

func schedulerKey(target routeTarget) string {
	if target.Binding.ID != 0 {
		return fmt.Sprintf("mb:%d", target.Binding.ID)
	}
	return fmt.Sprintf("m:%d:s:%d:sk:%d", target.Model.ID, target.Binding.SourceID, sourceKeyIDValueFromBinding(target.Binding))
}

func routeTargetLess(left routeTarget, right routeTarget, now time.Time) bool {
	if left.Source.Priority != right.Source.Priority {
		return left.Source.Priority < right.Source.Priority
	}
	leftWeight := effectiveRoutingWeight(left, now)
	rightWeight := effectiveRoutingWeight(right, now)
	if leftWeight != rightWeight {
		return leftWeight > rightWeight
	}
	if left.Binding.ID != right.Binding.ID {
		return left.Binding.ID < right.Binding.ID
	}
	if left.Model.ID != right.Model.ID {
		return left.Model.ID < right.Model.ID
	}
	if left.Source.ID != right.Source.ID {
		return left.Source.ID < right.Source.ID
	}
	return sourceKeyIDValueFromBinding(left.Binding) < sourceKeyIDValueFromBinding(right.Binding)
}

func (a *App) markBindingSuccess(target routeTarget, now time.Time) {
	if target.Binding.ID == 0 {
		return
	}
	var binding ModelRouteBinding
	if err := a.db.First(&binding, target.Binding.ID).Error; err != nil {
		return
	}
	state := schedulerBindingState(binding)
	updates := map[string]any{
		"cooldown_until":  nil,
		"last_success_at": now,
	}
	switch state {
	case schedulerStateObserving:
		updates["scheduler_state"] = schedulerStateObserving
	default:
		updates["scheduler_state"] = schedulerStateClosed
		updates["failure_count"] = 0
		updates["success_streak"] = 0
	}
	_ = a.db.Model(&ModelRouteBinding{}).Where("id = ?", binding.ID).Updates(updates).Error
	if updates["scheduler_state"] == schedulerStateClosed {
		a.resetSchedulerBindingMemory(binding.ID)
	}
}

func (a *App) markBindingFailure(target routeTarget, statusCode int, now time.Time) {
	if target.Binding.ID == 0 {
		return
	}
	result := a.db.Model(&ModelRouteBinding{}).Where("id = ?", target.Binding.ID).Updates(map[string]any{
		"failure_count":   gorm.Expr("failure_count + ?", 1),
		"success_streak":  0,
		"last_failure_at": now,
	})
	if result.Error != nil || result.RowsAffected != 1 {
		return
	}
	query := a.db.Model(&ModelRouteBinding{}).Where("id = ?", target.Binding.ID)
	if statusCode != http.StatusUnauthorized && statusCode != http.StatusForbidden {
		query = query.Where("failure_count >= ? OR scheduler_state IN ?", schedulerFailureThreshold, []string{schedulerStateHalfOpen, schedulerStateRecovering, schedulerStateObserving})
	}
	opened := query.Updates(map[string]any{
		"scheduler_state":   schedulerStateOpen,
		"cooldown_until":    now.Add(schedulerProbeInterval),
		"observation_until": nil,
	})
	if opened.Error == nil && opened.RowsAffected == 1 {
		a.resetSchedulerBindingMemory(target.Binding.ID)
	}
}

func (a *App) runSchedulerProbeLoop(done <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			a.runDueSchedulerProbes(now)
		case <-done:
			return
		}
	}
}

func (a *App) runDueSchedulerProbes(now time.Time) {
	var bindings []ModelRouteBinding
	if err := a.db.Where("scheduler_state IN ? AND cooldown_until IS NOT NULL AND cooldown_until <= ?", []string{schedulerStateOpen, schedulerStateRecovering, schedulerStateHalfOpen}, now).Find(&bindings).Error; err != nil {
		return
	}
	for _, binding := range bindings {
		if !a.claimSchedulerProbe(binding, now) {
			continue
		}
		go a.probeSchedulerBinding(binding.ID, now)
	}
}

func (a *App) claimSchedulerProbe(binding ModelRouteBinding, now time.Time) bool {
	leaseUntil := now.Add(schedulerProbeLeaseDuration)
	result := a.db.Model(&ModelRouteBinding{}).
		Where("id = ? AND scheduler_state = ? AND cooldown_until <= ? AND (probe_lease_until IS NULL OR probe_lease_until <= ?)", binding.ID, schedulerBindingState(binding), now, now).
		Updates(map[string]any{"scheduler_state": schedulerStateHalfOpen, "probe_lease_until": leaseUntil})
	return result.Error == nil && result.RowsAffected == 1
}

func (a *App) probeSchedulerBinding(bindingID uint, now time.Time) {
	target, protocol, err := a.schedulerProbeTarget(bindingID)
	if err != nil {
		a.markSchedulerProbeFailure(bindingID, now)
		return
	}
	path, body, err := modelInvokeTestPayload(protocol, target.Model.Name)
	if err != nil {
		a.markSchedulerProbeFailure(bindingID, now)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), schedulerProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL(target, protocol, path), bytes.NewReader(body))
	if err != nil {
		a.markSchedulerProbeFailure(bindingID, now)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	applyUpstreamAuth(req.Header, target.Source, effectiveUpstreamAPIKey(target), protocol)
	if protocol == relayProtocolAnthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	started := time.Now()
	resp, err := (&http.Client{Timeout: schedulerProbeTimeout}).Do(req)
	if err != nil {
		a.recordSchedulerProbe(target, false, 0, time.Since(started).Milliseconds(), err.Error())
		a.markSchedulerProbeFailure(bindingID, time.Now())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		a.recordSchedulerProbe(target, false, resp.StatusCode, time.Since(started).Milliseconds(), resp.Status)
		a.markSchedulerProbeFailure(bindingID, time.Now())
		return
	}
	a.recordSchedulerProbe(target, true, resp.StatusCode, time.Since(started).Milliseconds(), "")
	a.markSchedulerProbeSuccess(bindingID, time.Now())
}

// recordSchedulerProbe 持久化一次后台探测结果（独立于用户请求），并按模型限制保留条数。
func (a *App) recordSchedulerProbe(target routeTarget, success bool, statusCode int, latencyMS int64, message string) {
	log := SchedulerProbeLog{
		ModelID:     target.Model.ID,
		ModelName:   target.Model.Name,
		BindingID:   target.Binding.ID,
		SourceID:    target.Source.ID,
		SourceKeyID: sourceKeyIDFromTarget(target),
		Success:     success,
		StatusCode:  statusCode,
		LatencyMS:   latencyMS,
		Message:     message,
		ProbedAt:    time.Now(),
	}
	if err := a.db.Create(&log).Error; err != nil {
		return
	}
	var count int64
	if err := a.db.Model(&SchedulerProbeLog{}).Where("model_id = ?", target.Model.ID).Count(&count).Error; err != nil {
		return
	}
	if count > maxProbeLogsPerModel {
		excess := count - maxProbeLogsPerModel
		_ = a.db.Exec("DELETE FROM scheduler_probe_logs WHERE model_id = ? ORDER BY id ASC LIMIT ?", target.Model.ID, excess).Error
	}
}

// recentProbeLogsByModel 返回每个模型最近 10 条探测记录（按探测时间正序，旧→新）。
func (a *App) recentProbeLogsByModel(models []ModelConfig) map[uint][]SchedulerProbeLog {
	result := map[uint][]SchedulerProbeLog{}
	if len(models) == 0 {
		return result
	}
	ids := make([]uint, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	var logs []SchedulerProbeLog
	if err := a.db.Where("model_id IN ?", ids).Order("id desc").Limit(500).Find(&logs).Error; err != nil {
		return result
	}
	buckets := map[uint][]SchedulerProbeLog{}
	for _, log := range logs {
		if len(buckets[log.ModelID]) >= 10 {
			continue
		}
		buckets[log.ModelID] = append(buckets[log.ModelID], log)
	}
	for id, list := range buckets {
		sort.SliceStable(list, func(i, j int) bool { return list[i].ProbedAt.Before(list[j].ProbedAt) })
		result[id] = list
	}
	return result
}

// probeHistoryDTO 将探测记录转为对外 DTO（近 10 条，旧→新）。
func probeHistoryDTO(logs []SchedulerProbeLog) []gin.H {
	out := make([]gin.H, 0, len(logs))
	for _, log := range logs {
		out = append(out, gin.H{
			"success": log.Success,
			"at":      log.ProbedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func (a *App) schedulerProbeTarget(bindingID uint) (routeTarget, relayProtocol, error) {
	var binding ModelRouteBinding
	if err := a.db.First(&binding, bindingID).Error; err != nil {
		return routeTarget{}, "", err
	}
	var model ModelConfig
	if err := a.db.First(&model, binding.ModelID).Error; err != nil {
		return routeTarget{}, "", err
	}
	var source UpstreamSource
	if err := a.db.First(&source, binding.SourceID).Error; err != nil {
		return routeTarget{}, "", err
	}
	target := routeTarget{Model: model, Binding: binding, Source: source}
	if binding.SourceKeyID != nil {
		var key SourceKey
		if err := a.db.Where("id = ? AND source_id = ? AND status = ?", *binding.SourceKeyID, source.ID, APIKeyStatusValid).First(&key).Error; err != nil {
			return routeTarget{}, "", err
		}
		target.SourceKey = &key
	}
	return target, modelTestProtocol(model), nil
}

func (a *App) markSchedulerProbeSuccess(bindingID uint, now time.Time) {
	var binding ModelRouteBinding
	if err := a.db.First(&binding, bindingID).Error; err != nil {
		return
	}
	streak := binding.SuccessStreak + 1
	updates := map[string]any{"success_streak": streak, "last_success_at": now, "probe_lease_until": nil}
	if streak >= schedulerRecoverySuccessThreshold {
		updates["scheduler_state"] = schedulerStateObserving
		updates["observation_until"] = now.Add(schedulerObservationDuration)
		updates["cooldown_until"] = nil
	} else {
		updates["scheduler_state"] = schedulerStateRecovering
		updates["cooldown_until"] = now.Add(schedulerRecoveryProbeInterval)
	}
	_ = a.db.Model(&ModelRouteBinding{}).Where("id = ? AND scheduler_state = ?", bindingID, schedulerStateHalfOpen).Updates(updates).Error
}

func (a *App) markSchedulerProbeFailure(bindingID uint, now time.Time) {
	_ = a.db.Model(&ModelRouteBinding{}).Where("id = ?", bindingID).Updates(map[string]any{
		"scheduler_state": schedulerStateOpen, "success_streak": 0, "cooldown_until": now.Add(schedulerProbeInterval), "observation_until": nil, "probe_lease_until": nil, "last_failure_at": now,
	}).Error
	a.resetSchedulerBindingMemory(bindingID)
}
