package usage

// Summary represents aggregated usage summary.
type Summary struct {
	TotalRequests  int
	TotalTokens    int
	TotalFallbacks int
}

// RouteStats represents stats for a single route.
type RouteStats struct {
	Profile   string
	Route     string
	Requests  int
	Tokens    int
	Fallbacks int
}

// ModelStats represents stats for a single model.
type ModelStats struct {
	Profile  string
	Model    string
	Requests int
	Tokens   int
}

// AggregateSummary computes overall summary from records.
func AggregateSummary(records []*Record) Summary {
	var s Summary
	for _, r := range records {
		s.TotalRequests++
		s.TotalTokens += r.Tokens
		s.TotalFallbacks += r.Fallbacks
	}
	return s
}

// AggregateByRoute groups records by profile/route.
func AggregateByRoute(records []*Record) map[string]*RouteStats {
	result := make(map[string]*RouteStats)
	for _, r := range records {
		profile := r.Profile
		if profile == "" {
			profile = "default"
		}
		key := profile + "/" + r.Route
		stats, ok := result[key]
		if !ok {
			stats = &RouteStats{Profile: profile, Route: r.Route}
			result[key] = stats
		}
		stats.Requests++
		stats.Tokens += r.Tokens
		stats.Fallbacks += r.Fallbacks
	}
	return result
}

// AggregateByModel groups records by profile/provider.model.
func AggregateByModel(records []*Record) map[string]*ModelStats {
	result := make(map[string]*ModelStats)
	for _, r := range records {
		profile := r.Profile
		if profile == "" {
			profile = "default"
		}
		key := profile + "/" + r.Provider + "." + r.Model
		stats, ok := result[key]
		if !ok {
			stats = &ModelStats{Profile: profile, Model: r.Provider + "." + r.Model}
			result[key] = stats
		}
		stats.Requests++
		stats.Tokens += r.Tokens
	}
	return result
}
