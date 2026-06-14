package usage

// Summary represents aggregated usage summary.
type Summary struct {
	TotalRequests  int
	TotalTokens    int
	TotalFallbacks int
	UniqueUsers    int
	UniqueGroups   int
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
	users := make(map[string]struct{})
	groups := make(map[string]struct{})
	for _, r := range records {
		s.TotalRequests++
		s.TotalTokens += r.Tokens
		s.TotalFallbacks += r.Fallbacks
		if r.UserName != "" {
			users[r.UserName] = struct{}{}
		}
		if r.GroupName != "" {
			groups[r.GroupName] = struct{}{}
		}
	}
	s.UniqueUsers = len(users)
	s.UniqueGroups = len(groups)
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

// UserStats represents stats for a single user.
type UserStats struct {
	UserName  string
	GroupName string
	Requests  int
	Tokens    int
	Fallbacks int
}

// GroupStats represents stats for a single group.
type GroupStats struct {
	GroupName string
	Requests  int
	Tokens    int
	Fallbacks int
	Users     int
}

// AggregateByUser groups records by user name.
func AggregateByUser(records []*Record) map[string]*UserStats {
	result := make(map[string]*UserStats)
	for _, r := range records {
		name := r.UserName
		if name == "" {
			name = "(anonymous)"
		}
		stats, ok := result[name]
		if !ok {
			groupName := r.GroupName
			if groupName == "" {
				groupName = "(default)"
			}
			stats = &UserStats{UserName: name, GroupName: groupName}
			result[name] = stats
		}
		stats.Requests++
		stats.Tokens += r.Tokens
		stats.Fallbacks += r.Fallbacks
		// Carry the latest non-empty group name
		if r.GroupName != "" {
			stats.GroupName = r.GroupName
		}
	}
	return result
}

// AggregateByGroup groups records by group name and counts distinct users per group.
func AggregateByGroup(records []*Record) map[string]*GroupStats {
	result := make(map[string]*GroupStats)
	groupUsers := make(map[string]map[string]struct{})
	for _, r := range records {
		groupName := r.GroupName
		if groupName == "" {
			groupName = "(default)"
		}
		stats, ok := result[groupName]
		if !ok {
			stats = &GroupStats{GroupName: groupName}
			result[groupName] = stats
			groupUsers[groupName] = make(map[string]struct{})
		}
		stats.Requests++
		stats.Tokens += r.Tokens
		stats.Fallbacks += r.Fallbacks
		if r.UserName != "" {
			groupUsers[groupName][r.UserName] = struct{}{}
		}
	}
	for groupName, stats := range result {
		stats.Users = len(groupUsers[groupName])
	}
	return result
}
