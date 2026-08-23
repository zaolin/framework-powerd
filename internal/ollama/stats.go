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

// ModelDetails holds the model metadata returned by Ollama's /api/ps endpoint.
type ModelDetails struct {
	ParentModel       string   `json:"parent_model"`
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// ModelInfo holds the full per-model data from Ollama's /api/ps response.
// Matches the official API spec at https://docs.ollama.com/api/ps.
type ModelInfo struct {
	Name          string       `json:"name"`
	Model         string       `json:"model"`
	Size          int64        `json:"size"`
	SizeVRAM      int64        `json:"size_vram"`
	Digest        string       `json:"digest"`
	Details       ModelDetails `json:"details"`
	ExpiresAt     string       `json:"expires_at"`
	ContextLength int          `json:"context_length"`
}

// Stats holds the complete statistics structure
type Stats struct {
	ByIP            map[string]RequestStats `json:"by_ip"`
	ByGroup         map[string]RequestStats `json:"by_group"`
	Ungrouped       RequestStats            `json:"ungrouped"`
	Currency        string                  `json:"currency"`
	PricePerKWh     float64                 `json:"price_per_kwh"`
	Since           time.Time               `json:"since"`
	Models          []ModelInfo             `json:"models"`
	LoadedVRAMBytes int64                   `json:"loaded_vram_bytes"`
	OllamaVersion   string                  `json:"ollama_version"`
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
		Models:      []ModelInfo{},
	}
}
