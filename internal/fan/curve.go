package fan

import (
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

// params is the normalized curve configuration.
type params struct {
	Min, Start, Full        int
	Critical, BoardCritical int
	Hysteresis, Fall        int
	Dwell                   time.Duration
}

// curveState is the controller memory between updates.
type curveState struct {
	Duty         int
	LastIncrease time.Time
}

// dutyForTemp maps a temperature to a duty along the ramp.
func dutyForTemp(temp float64, p params) int {
	if temp <= float64(p.Start) {
		return p.Min
	}
	if temp >= float64(p.Full) {
		return 100
	}
	span := float64(p.Full - p.Start)
	frac := (temp - float64(p.Start)) / span
	duty := float64(p.Min) + frac*float64(100-p.Min)
	if duty > 100 {
		duty = 100
	}
	return int(duty + 0.5)
}

// tempForDuty is the inverse of dutyForTemp, used for the hysteresis band.
func tempForDuty(duty int, p params) float64 {
	if duty <= p.Min {
		return float64(p.Start)
	}
	frac := float64(duty-p.Min) / float64(100-p.Min)
	return float64(p.Start) + frac*float64(p.Full-p.Start)
}

// nextDuty advances the curve by one step.
//
// Rises are immediate. Falls are limited to one step, require the dwell time
// since the last rise, and only happen once the temperature has dropped a full
// hysteresis band below the current duty.
func nextDuty(s SensorSet, p params, prev curveState, now time.Time) (int, curveState) {
	if s.Max >= float64(p.Critical) || (s.Board != nil && *s.Board >= float64(p.BoardCritical)) {
		return 100, curveState{Duty: 100, LastIncrease: now}
	}

	target := dutyForTemp(s.Max, p)
	if target > prev.Duty {
		return target, curveState{Duty: target, LastIncrease: now}
	}
	if target == prev.Duty {
		return prev.Duty, prev
	}
	if now.Sub(prev.LastIncrease) < p.Dwell {
		return prev.Duty, prev
	}
	if s.Max > tempForDuty(prev.Duty, p)-float64(p.Hysteresis) {
		return prev.Duty, prev
	}

	duty := prev.Duty - p.Fall
	if duty < target {
		duty = target
	}
	if duty < p.Min {
		duty = p.Min
	}
	return duty, curveState{Duty: duty, LastIncrease: prev.LastIncrease}
}

// paramsFromConfig normalizes the configuration for the curve.
func paramsFromConfig(cfg config.FanConfig) params {
	return params{
		Min:           cfg.GetMinDutyPercent(),
		Start:         cfg.GetStartTempC(),
		Full:          cfg.GetFullTempC(),
		Critical:      cfg.GetCriticalTempC(),
		BoardCritical: cfg.GetBoardCriticalTempC(),
		Hysteresis:    cfg.GetHysteresisC(),
		Fall:          cfg.GetFallStepPercent(),
		Dwell:         90 * time.Second,
	}
}
