package ollama

import "time"

// RequestInfo holds parsed data from a single GIN log line
type RequestInfo struct {
	Timestamp time.Time
	IP        string
	Method    string
	Endpoint  string
	Status    int
	Duration  time.Duration
}

// RequestStats holds aggregated statistics
type RequestStats struct {
	Count         int            `json:"count"`
	TotalDuration float64        `json:"total_duration_secs"`
	TotalEnergy   float64        `json:"total_energy_kwh"`
	TotalCost     float64        `json:"total_cost"`
	Endpoints     map[string]int `json:"endpoints"`
	LastRequest   time.Time      `json:"last_request"`
}

// NewRequestStats creates an initialized RequestStats
func NewRequestStats() RequestStats {
	return RequestStats{
		Endpoints: make(map[string]int),
	}
}

// Add incorporates a request into the stats
func (s *RequestStats) Add(info *RequestInfo, energyKWh, cost float64) {
	s.Count++
	s.TotalDuration += info.Duration.Seconds()
	s.TotalEnergy += energyKWh
	s.TotalCost += cost
	s.Endpoints[info.Endpoint]++
	s.LastRequest = info.Timestamp
}

// Stats holds the complete statistics structure
type Stats struct {
	ByIP        map[string]RequestStats `json:"by_ip"`
	ByGroup     map[string]RequestStats `json:"by_group"`
	Ungrouped   RequestStats            `json:"ungrouped"`
	Currency    string                  `json:"currency"`
	PricePerKWh float64                 `json:"price_per_kwh"`
	Since       time.Time               `json:"since"`
}

// NewStats creates an initialized Stats
func NewStats(pricePerKWh float64, currency string) Stats {
	return Stats{
		ByIP:        make(map[string]RequestStats),
		ByGroup:     make(map[string]RequestStats),
		Ungrouped:   NewRequestStats(),
		Currency:    currency,
		PricePerKWh: pricePerKWh,
		Since:       time.Now(),
	}
}
