package app

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/tracking/sessionusage"
)

type dashboardAggregate struct {
	UsageDashboardMetrics
	sessions                               map[string]int64
	projects, credentials, dates, unpriced map[string]bool
	duration                               int64
	durationCount                          int64
}

func newDashboardAggregate() *dashboardAggregate {
	return &dashboardAggregate{sessions: map[string]int64{}, projects: map[string]bool{}, credentials: map[string]bool{}, dates: map[string]bool{}, unpriced: map[string]bool{}}
}

func dashboardKey(parts ...string) string        { data, _ := json.Marshal(parts); return string(data) }
func sessionKey(provider, session string) string { return dashboardKey(provider, session) }
func credentialKey(provider, channel, group string) string {
	return dashboardKey(provider, channel, group)
}

func (a *dashboardAggregate) add(record dashboardRecord, loc *time.Location) {
	a.Requests++
	a.Messages++ // a response with usage is one model message/call
	a.InputTokens += record.Input
	a.OutputTokens += record.Output
	a.CacheReadTokens += record.CacheRead
	a.CacheCreationTokens += record.CacheWrite
	a.Tokens += record.Input + record.Output
	a.CostUSD += record.cost
	if !record.priced {
		a.Unpriced++
		a.unpriced[record.Provider+" · "+displayModel(record.Model)] = true
	}
	if record.Session != "" {
		a.sessions[sessionKey(record.Provider, record.Session)] += record.Input + record.Output
	}
	if record.Project != "" {
		a.projects[record.Project] = true
	}
	if record.channel != "" || record.group != "" {
		a.credentials[credentialKey(record.Provider, record.channel, record.group)] = true
	}
	a.dates[record.At.In(loc).Format(time.DateOnly)] = true
	if record.status == "completed" {
		a.SuccessRequests++
	} else if record.status != "" && record.status != "pending" && record.status != "processing" && record.status != "streaming" {
		a.FailedRequests++
	}
	if record.duration > 0 {
		a.duration += record.duration
		a.durationCount++
	}
}

func (a *dashboardAggregate) addActivity(item sessionusage.Activity, loc *time.Location) {
	a.Messages += item.Prompts
	a.ActiveSeconds += float64(item.Milliseconds) / 1000
	key := sessionKey(item.Provider, item.Session)
	if _, exists := a.sessions[key]; !exists {
		a.sessions[key] = 0
	}
	if item.Project != "" {
		a.projects[item.Project] = true
	}
	a.dates[item.At.In(loc).Format(time.DateOnly)] = true
}

func (a *dashboardAggregate) metrics() UsageDashboardMetrics {
	m := a.UsageDashboardMetrics
	m.Sessions, m.Projects, m.CredentialGroups, m.ActiveDays = int64(len(a.sessions)), int64(len(a.projects)), int64(len(a.credentials)), int64(len(a.dates))
	if a.durationCount > 0 {
		m.AvgDurationMs = float64(a.duration) / float64(a.durationCount)
	}
	if denominator := m.InputTokens + m.CacheReadTokens + m.CacheCreationTokens; denominator > 0 {
		value := float64(m.CacheReadTokens) / float64(denominator)
		m.CacheHitRate = &value
	}
	spent := make([]int64, 0, len(a.sessions))
	for _, tokens := range a.sessions {
		spent = append(spent, tokens)
	}
	sort.Slice(spent, func(i, j int) bool { return spent[i] < spent[j] })
	if len(spent) > 0 {
		m.MedianTokens = spent[max(0, int(math.Ceil(.5*float64(len(spent))))-1)]
		m.P90Tokens = spent[max(0, int(math.Ceil(.9*float64(len(spent))))-1)]
	}
	return m
}

func displayModel(model string) string {
	if model == "" {
		return "未记录模型"
	}
	return model
}
func displayProject(project string) string {
	if project == "" {
		return "未记录项目"
	}
	parts := strings.Split(strings.ReplaceAll(project, "\\", "/"), "/")
	return parts[len(parts)-1]
}

type dashboardBucket struct {
	group UsageDashboardGroup
	total *dashboardAggregate
}

func groupBucket(groups map[string]*dashboardBucket, key, name, provider, channel, group string) *dashboardAggregate {
	if groups[key] == nil {
		groups[key] = &dashboardBucket{UsageDashboardGroup{Key: key, Name: name, Provider: provider, Channel: channel, Group: group}, newDashboardAggregate()}
	}
	return groups[key].total
}
func dashboardGroups(groups map[string]*dashboardBucket) []UsageDashboardGroup {
	result := make([]UsageDashboardGroup, 0, len(groups))
	for _, bucket := range groups {
		item := bucket.group
		item.UsageDashboardMetrics = bucket.total.metrics()
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Tokens != result[j].Tokens {
			return result[i].Tokens > result[j].Tokens
		}
		if result[i].CostUSD != result[j].CostUSD {
			return result[i].CostUSD > result[j].CostUSD
		}
		return result[i].Key < result[j].Key
	})
	return result
}

type dashboardRowBucket struct {
	row         UsageDashboardRow
	total       *dashboardAggregate
	models      map[string]bool
	first, last time.Time
}

func buildUsageDashboard(query UsageDashboardQuery, start, end, now time.Time, records []dashboardRecord, activity []sessionusage.Activity) UsageDashboard {
	loc := now.Location()
	calendarStart, _ := heatmapCalendarRange(now)
	// "All" starts at the first observed record, not 1970. Empty data still
	// produces today's zero bucket rather than an unbounded empty timeline.
	if query.Period == "all" {
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		for _, record := range records {
			if record.At.Before(start) {
				t := record.At.In(loc)
				start = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
			}
		}
		for _, item := range activity {
			if item.At.Before(start) {
				t := item.At.In(loc)
				start = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
			}
		}
	}
	out := UsageDashboard{Source: query.Source, From: start.Format(time.DateOnly), To: end.Add(-time.Nanosecond).Format(time.DateOnly), Timezone: loc.String(), UpdatedAt: now.Format(time.RFC3339), Page: query.Page, PageSize: query.PageSize, Rows: []UsageDashboardRow{}}
	total := newDashboardAggregate()
	daily, calendar := map[string]*dashboardAggregate{}, map[string]*dashboardAggregate{}
	projects, models, providers, credentials := map[string]*dashboardBucket{}, map[string]*dashboardBucket{}, map[string]*dashboardBucket{}, map[string]*dashboardBucket{}
	projectLatest := map[string]time.Time{}
	rememberProject := func(project, title string, at time.Time) {
		if bucket := projects[project]; bucket != nil && (at.After(projectLatest[project]) || at.Equal(projectLatest[project]) && bucket.group.Title == "") {
			projectLatest[project] = at
			bucket.group.Title, bucket.group.LastAt = title, at.In(loc).Format(time.RFC3339Nano)
		}
	}
	modelOptions, projectOptions := map[string]bool{}, map[string]bool{}
	credentialOptions := map[string]*dashboardBucket{}
	rows := map[string]*dashboardRowBucket{}
	dayBucket := func(buckets map[string]*dashboardAggregate, date string) *dashboardAggregate {
		if buckets[date] == nil {
			buckets[date] = newDashboardAggregate()
		}
		return buckets[date]
	}
	inPeriod := func(at time.Time) bool { return !at.Before(start) && at.Before(end) }
	term := strings.ToLower(query.Search)
	selectedProject := query.Project
	if selectedProject == "\x00" {
		selectedProject = ""
	} // NUL cannot be a filesystem path; selects unrecorded projects.
	matches := func(provider, model, project, channel, group, id, title string) bool {
		return (query.Provider == "" || query.Provider == provider) && (query.Model == "" || strings.EqualFold(query.Model, model)) && (query.Project == "" || selectedProject == project) && (query.Credential == "" || query.Credential == credentialKey(provider, channel, group)) && (term == "" || strings.Contains(strings.ToLower(strings.Join([]string{title, model, project, channel, group, id}, " ")), term))
	}
	rowBucket := func(id, provider, project, channel, group, model string, at time.Time) *dashboardRowBucket {
		if rows[id] == nil {
			rows[id] = &dashboardRowBucket{row: UsageDashboardRow{ID: id, Provider: provider, Project: project, Channel: channel, Group: group}, total: newDashboardAggregate(), models: map[string]bool{}, first: at, last: at}
		}
		bucket := rows[id]
		if at.Before(bucket.first) {
			bucket.first = at
		}
		if at.After(bucket.last) {
			bucket.last = at
		}
		if model != "" {
			bucket.models[model] = true
		}
		return bucket
	}
	for _, record := range records {
		if query.Provider != "" && query.Provider != record.Provider {
			continue
		}
		if inPeriod(record.At) {
			modelOptions[record.Model] = true
			if record.Project != "" {
				projectOptions[record.Project] = true
			}
			if query.Source == "proxy" {
				key := credentialKey(record.Provider, record.channel, record.group)
				groupBucket(credentialOptions, key, displayCredential(record.channel, record.group), record.Provider, record.channel, record.group)
			}
		}
		identity := record.Session
		if query.Source == "proxy" {
			identity = record.ID
		}
		if !matches(record.Provider, record.Model, record.Project, record.channel, record.group, identity, record.Title) {
			continue
		}
		date := record.At.In(loc).Format(time.DateOnly)
		if !record.At.Before(calendarStart) && record.At.Before(end) {
			dayBucket(calendar, date).add(record, loc)
		}
		if inPeriod(record.At) {
			dayBucket(daily, date).add(record, loc)
		}
		// A calendar selection filters the detail view independently from the
		// trend period; it must not require loading all historical records.
		if query.Day != "" && query.Day != date || query.Day == "" && !inPeriod(record.At) {
			continue
		}
		total.add(record, loc)
		groupBucket(models, dashboardKey(record.Provider, record.Model), displayModel(record.Model), record.Provider, "", "").add(record, loc)
		groupBucket(providers, record.Provider, record.Provider, record.Provider, "", "").add(record, loc)
		if query.Source == "local" {
			groupBucket(projects, record.Project, displayProject(record.Project), "", "", "").add(record, loc)
			rememberProject(record.Project, record.Title, record.At)
		} else {
			groupBucket(credentials, credentialKey(record.Provider, record.channel, record.group), displayCredential(record.channel, record.group), record.Provider, record.channel, record.group).add(record, loc)
		}
		id := record.ID
		if query.Source == "local" {
			id = sessionKey(record.Provider, record.Session)
		}
		bucket := rowBucket(id, record.Provider, record.Project, record.channel, record.group, record.Model, record.At)
		bucket.total.add(record, loc)
		if record.Title != "" {
			bucket.row.Title = record.Title
		}
		bucket.row.Status = record.status
	}
	for _, item := range activity {
		if query.Provider == "" || query.Provider == item.Provider {
			if inPeriod(item.At) {
				if item.Model != "" {
					modelOptions[item.Model] = true
				}
				if item.Project != "" {
					projectOptions[item.Project] = true
				}
			}
		}
		if !matches(item.Provider, item.Model, item.Project, "", "", item.Session, item.Title) {
			continue
		}
		date := item.At.In(loc).Format(time.DateOnly)
		if !item.At.Before(calendarStart) && item.At.Before(end) {
			dayBucket(calendar, date).addActivity(item, loc)
		}
		if inPeriod(item.At) {
			dayBucket(daily, date).addActivity(item, loc)
		}
		if query.Day != "" && query.Day != date || query.Day == "" && !inPeriod(item.At) {
			continue
		}
		total.addActivity(item, loc)
		groupBucket(projects, item.Project, displayProject(item.Project), "", "", "").addActivity(item, loc)
		rememberProject(item.Project, item.Title, item.At)
		groupBucket(providers, item.Provider, item.Provider, item.Provider, "", "").addActivity(item, loc)
		groupBucket(models, dashboardKey(item.Provider, item.Model), displayModel(item.Model), item.Provider, "", "").addActivity(item, loc)
		bucket := rowBucket(sessionKey(item.Provider, item.Session), item.Provider, item.Project, "", "", item.Model, item.At)
		bucket.total.addActivity(item, loc)
		if item.Title != "" {
			bucket.row.Title = item.Title
		}
	}
	out.Summary = total.metrics()
	out.Projects, out.Models, out.Providers, out.Credentials = dashboardGroups(projects), dashboardGroups(models), dashboardGroups(providers), dashboardGroups(credentials)
	out.ModelOptions, out.ProjectOptions, out.UnpricedModels = sortedKeys(modelOptions), sortedKeys(projectOptions), sortedKeys(total.unpriced)
	out.CredentialOptions = dashboardGroups(credentialOptions)
	fillDays := func(from, to time.Time, buckets map[string]*dashboardAggregate) []UsageDashboardDay {
		days := make([]UsageDashboardDay, 0)
		for cursor := from; cursor.Before(to); cursor = cursor.AddDate(0, 0, 1) {
			date := cursor.Format(time.DateOnly)
			days = append(days, UsageDashboardDay{Date: date, UsageDashboardMetrics: dayBucket(buckets, date).metrics()})
		}
		return days
	}
	out.Days = fillDays(start, end, daily)
	out.Calendar = fillDays(calendarStart, end, calendar)
	allRows := make([]UsageDashboardRow, 0, len(rows))
	for _, bucket := range rows {
		row := bucket.row
		row.Models = sortedKeys(bucket.models)
		row.FirstAt = bucket.first.In(loc).Format(time.RFC3339Nano)
		row.LastAt = bucket.last.In(loc).Format(time.RFC3339Nano)
		row.UsageDashboardMetrics = bucket.total.metrics()
		allRows = append(allRows, row)
	}
	sort.Slice(allRows, func(i, j int) bool {
		a, b := allRows[i], allRows[j]
		switch query.Sort {
		case "tokens":
			if a.Tokens != b.Tokens {
				return a.Tokens > b.Tokens
			}
		case "cost":
			if a.CostUSD != b.CostUSD {
				return a.CostUSD > b.CostUSD
			}
		case "active":
			if a.ActiveSeconds != b.ActiveSeconds {
				return a.ActiveSeconds > b.ActiveSeconds
			}
		}
		if !rows[a.ID].last.Equal(rows[b.ID].last) {
			return rows[a.ID].last.After(rows[b.ID].last)
		}
		return a.ID < b.ID
	})
	out.RowCount = len(allRows)
	pages := max(1, (len(allRows)+query.PageSize-1)/query.PageSize)
	out.Page = min(query.Page, pages)
	from := min((out.Page-1)*out.PageSize, len(allRows))
	to := min(from+out.PageSize, len(allRows))
	out.Rows = allRows[from:to]
	return out
}

func sortedKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		if key != "" {
			result = append(result, key)
		}
	}
	sort.Strings(result)
	return result
}
func displayCredential(channel, group string) string {
	if channel == "" {
		channel = "未记录渠道"
	}
	if group == "" {
		group = "未记录分组"
	}
	return channel + " / " + group
}
