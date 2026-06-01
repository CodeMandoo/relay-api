package app

import (
	"fmt"
	"sort"
	"time"
)

const (
	schedulerStateClosed     = "closed"
	schedulerStateOpen       = "open"
	schedulerStateHalfOpen   = "half_open"
	schedulerStateRecovering = "recovering"

	schedulerShortCooldown            = 30 * time.Second
	schedulerMediumCooldown           = 2 * time.Minute
	schedulerLongCooldown             = 10 * time.Minute
	schedulerRecoveringWeightPercent  = 30
	schedulerRecoverySuccessThreshold = 3
)

func schedulerBindingState(binding ModelRouteBinding) string {
	switch binding.SchedulerState {
	case schedulerStateOpen, schedulerStateHalfOpen, schedulerStateRecovering:
		return binding.SchedulerState
	default:
		return schedulerStateClosed
	}
}

func schedulerResetUpdates() map[string]any {
	return map[string]any{
		"scheduler_state": schedulerStateClosed,
		"failure_count":   0,
		"success_streak":  0,
		"cooldown_until":  nil,
		"last_failure_at": nil,
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
	if binding.ID == 0 || state != schedulerStateOpen {
		return binding
	}
	if binding.CooldownUntil != nil && binding.CooldownUntil.After(now) {
		return binding
	}
	updates := map[string]any{
		"scheduler_state": schedulerStateHalfOpen,
		"cooldown_until":  nil,
	}
	if err := a.db.Model(&ModelRouteBinding{}).Where("id = ?", binding.ID).Updates(updates).Error; err == nil {
		binding.SchedulerState = schedulerStateHalfOpen
		binding.CooldownUntil = nil
	}
	return binding
}

func effectiveRoutingWeight(target routeTarget, now time.Time) int {
	if !target.Binding.Enabled {
		return 0
	}
	state := schedulerBindingState(target.Binding)
	if state == schedulerStateOpen {
		if target.Binding.CooldownUntil == nil || !target.Binding.CooldownUntil.After(now) {
			return 1
		}
		return 0
	}
	base := nonZeroInt(target.Binding.RoutingWeight, 1)
	switch state {
	case schedulerStateHalfOpen:
		return 1
	case schedulerStateRecovering:
		weight := base * schedulerRecoveringWeightPercent / 100
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
	if len(eligible) <= 1 {
		return eligible
	}

	totalWeight := 0
	for _, target := range eligible {
		totalWeight += effectiveRoutingWeight(target, now)
	}
	if totalWeight <= 0 {
		return nil
	}

	a.schedulerMu.Lock()
	if a.schedulerCurrent == nil {
		a.schedulerCurrent = map[string]int{}
	}
	selected := 0
	bestCurrent := 0
	for i, target := range eligible {
		key := schedulerKey(target)
		next := a.schedulerCurrent[key] + effectiveRoutingWeight(target, now)
		a.schedulerCurrent[key] = next
		if i == 0 || next > bestCurrent || (next == bestCurrent && routeTargetLess(target, eligible[selected], now)) {
			selected = i
			bestCurrent = next
		}
	}
	a.schedulerCurrent[schedulerKey(eligible[selected])] -= totalWeight
	a.schedulerMu.Unlock()

	ordered := make([]routeTarget, 0, len(eligible))
	ordered = append(ordered, eligible[selected])
	remaining := make([]routeTarget, 0, len(eligible)-1)
	for i, target := range eligible {
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

func schedulerCooldownForFailures(failures int) time.Duration {
	switch {
	case failures >= 4:
		return schedulerLongCooldown
	case failures == 3:
		return schedulerMediumCooldown
	case failures == 2:
		return schedulerShortCooldown
	default:
		return 0
	}
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
	case schedulerStateHalfOpen:
		updates["scheduler_state"] = schedulerStateRecovering
		updates["success_streak"] = 1
	case schedulerStateRecovering:
		streak := binding.SuccessStreak + 1
		if streak >= schedulerRecoverySuccessThreshold {
			updates["scheduler_state"] = schedulerStateClosed
			updates["failure_count"] = 0
			updates["success_streak"] = 0
		} else {
			updates["scheduler_state"] = schedulerStateRecovering
			updates["success_streak"] = streak
		}
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

func (a *App) markBindingFailure(target routeTarget, now time.Time) {
	if target.Binding.ID == 0 {
		return
	}
	var binding ModelRouteBinding
	if err := a.db.First(&binding, target.Binding.ID).Error; err != nil {
		return
	}
	failures := binding.FailureCount + 1
	updates := map[string]any{
		"failure_count":   failures,
		"success_streak":  0,
		"last_failure_at": now,
	}
	if target.SingleSource {
		updates["scheduler_state"] = schedulerStateClosed
		updates["cooldown_until"] = nil
		_ = a.db.Model(&ModelRouteBinding{}).Where("id = ?", binding.ID).Updates(updates).Error
		a.resetSchedulerBindingMemory(binding.ID)
		return
	}

	state := schedulerBindingState(binding)
	if state == schedulerStateHalfOpen || state == schedulerStateRecovering {
		updates["scheduler_state"] = schedulerStateOpen
		updates["cooldown_until"] = now.Add(schedulerLongCooldown)
		_ = a.db.Model(&ModelRouteBinding{}).Where("id = ?", binding.ID).Updates(updates).Error
		a.resetSchedulerBindingMemory(binding.ID)
		return
	}

	cooldown := schedulerCooldownForFailures(failures)
	if cooldown > 0 {
		updates["scheduler_state"] = schedulerStateOpen
		updates["cooldown_until"] = now.Add(cooldown)
	} else {
		updates["scheduler_state"] = schedulerStateClosed
		updates["cooldown_until"] = nil
	}
	_ = a.db.Model(&ModelRouteBinding{}).Where("id = ?", binding.ID).Updates(updates).Error
	if cooldown > 0 {
		a.resetSchedulerBindingMemory(binding.ID)
	}
}
