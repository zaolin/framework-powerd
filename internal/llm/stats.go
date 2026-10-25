package llm

import "time"

// RequestStats holds aggregated statistics for one IP or group.
type RequestStats struct {
	Count         int            `json:"count"`
	TotalDuration float64        `json:"total_duration_secs"`
	TotalEnergy   float64        `json:"total_energy_kwh"`
	TotalCost     float64        `json:"total_cost"`
	Endpoints     map[string]int `json:"endpoints"`
	FirstRequest  time.Time      `json:"first_request"`
	LastRequest   time.Time      `json:"last_request"`
}

func newRequestStats() RequestStats {
	return RequestStats{Endpoints: make(map[string]int)}
}

// Add incorporates a request into the stats.
func (s *RequestStats) Add(endpoint string, duration time.Duration, energyKWh, cost float64, at time.Time) {
	s.Count++
	s.TotalDuration += duration.Seconds()
	s.TotalEnergy += energyKWh
	s.TotalCost += cost
	if s.Endpoints == nil {
		s.Endpoints = make(map[string]int)
	}
	s.Endpoints[endpoint]++
	if s.FirstRequest.IsZero() {
		s.FirstRequest = at
	}
	s.LastRequest = at
}

// DayStats holds the per-day request counters.
type DayStats struct {
	Date        string         `json:"date"`
	Total       int            `json:"total"`
	TotalEnergy float64        `json:"total_energy_kwh"`
	TotalCost   float64        `json:"total_cost"`
	ByIP        map[string]int `json:"by_ip"`
	ByGroup     map[string]int `json:"by_group"`
}

func newDayStats(date string) DayStats {
	return DayStats{
		Date:    date,
		ByIP:    make(map[string]int),
		ByGroup: make(map[string]int),
	}
}

// Stats is the complete LLM accounting state.
type Stats struct {
	Enabled     bool                    `json:"enabled"`
	Since       time.Time               `json:"since"`
	Total       RequestStats            `json:"total"`
	ByIP        map[string]RequestStats `json:"by_ip"`
	ByGroup     map[string]RequestStats `json:"by_group"`
	Ungrouped   RequestStats            `json:"ungrouped"`
	Days        []DayStats              `json:"days"`
	Today       DayStats                `json:"today"`
	Currency    string                  `json:"currency"`
	PricePerKWh float64                 `json:"price_per_kwh"`
	LastError   string                  `json:"last_error,omitempty"`
	LastRequest time.Time               `json:"last_request"`
}

// NewStats creates an initialized Stats.
func NewStats(pricePerKWh float64, currency string) Stats {
	return Stats{
		ByIP:        make(map[string]RequestStats),
		ByGroup:     make(map[string]RequestStats),
		Ungrouped:   newRequestStats(),
		Since:       time.Now(),
		Currency:    currency,
		PricePerKWh: pricePerKWh,
	}
}
