package tracking

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (ut *UsageTracker) formatStartTimeQueryBound(t time.Time) string {
	if ut != nil && ut.location != nil {
		t = t.In(ut.location)
	}
	return t.Format("2006-01-02 15:04:05")
}

func (ut *UsageTracker) formatEndTimeQueryBound(t time.Time) string {
	if ut != nil && ut.location != nil {
		t = t.In(ut.location)
	}
	// DB 中 start_time 可能被写为：
	// - "YYYY-MM-DD HH:MM:SS"
	// - "YYYY-MM-DD HH:MM:SS.ffffff"
	// - "YYYY-MM-DD HH:MM:SS.fffffffff"
	// 这里统一用一个带小数的“上界”字符串，保证 <= 查询不漏掉带小数的记录。
	return t.Format("2006-01-02 15:04:05") + ".999999999"
}

func (ut *UsageTracker) scopedQueryOptions(opts *QueryOptions) *QueryOptions {
	if opts == nil {
		opts = &QueryOptions{}
	}
	scoped := *opts
	if scoped.Environment == "" {
		scoped.Environment = normalizeTrackingEnvironment(ut.environment)
	}
	return &scoped
}

func queryEnvironment(opts *QueryOptions) string {
	if opts == nil {
		return "release"
	}
	return normalizeTrackingEnvironment(opts.Environment)
}

func queryModelNames(opts *QueryOptions) []string {
	if opts == nil {
		return nil
	}
	seen := make(map[string]bool)
	var names []string
	for _, name := range opts.ModelNames {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if len(names) == 0 && opts.ModelName != "" {
		names = append(names, opts.ModelName)
	}
	sort.Strings(names)
	return names
}

func queryModelCacheKey(opts *QueryOptions) string {
	encoded, _ := json.Marshal(queryModelNames(opts))
	return string(encoded)
}

func appendModelQueryFilter(query string, args []any, opts *QueryOptions) (string, []any) {
	names := queryModelNames(opts)
	if len(names) == 0 {
		return query, args
	}
	query += " AND model_name IN (" + strings.TrimSuffix(strings.Repeat("?,", len(names)), ",") + ")"
	for _, name := range names {
		args = append(args, name)
	}
	return query, args
}

// requestLogsHasColumn 月度归档库的表结构复制自归档当时的活动库，可能缺后加的列
func requestLogsHasColumn(ctx context.Context, db *sql.DB, column string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('request_logs') WHERE name = ?`, column,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to inspect request_logs schema: %w", err)
	}
	return count > 0, nil
}

func (ut *UsageTracker) appendRequestQueryFilters(query string, args []any, opts *QueryOptions) (string, []any) {
	if opts == nil {
		return query, args
	}
	query += " AND COALESCE(environment, 'release') = ?"
	args = append(args, queryEnvironment(opts))
	if opts.StartDate != nil {
		query += " AND start_time >= ?"
		args = append(args, ut.formatStartTimeQueryBound(*opts.StartDate))
	}
	if opts.EndDate != nil {
		query += " AND start_time <= ?"
		args = append(args, ut.formatEndTimeQueryBound(*opts.EndDate))
	}
	query, args = appendModelQueryFilter(query, args, opts)
	if opts.Channel != "" {
		query += " AND channel = ?"
		args = append(args, opts.Channel)
	}
	if opts.EndpointName != "" {
		query += " AND endpoint_name = ?"
		args = append(args, opts.EndpointName)
	}
	if opts.GroupName != "" {
		query += " AND group_name = ?"
		args = append(args, opts.GroupName)
	}
	if opts.Status == "failed" {
		query += " AND status IN ('failed', 'partial_failed', 'error', 'auth_error', 'rate_limited', 'server_error', 'network_error', 'stream_error', 'timeout')"
	} else if opts.Status != "" {
		query += " AND status = ?"
		args = append(args, opts.Status)
	}
	if opts.Provider != "" {
		query += " AND provider = ?"
		args = append(args, opts.Provider)
	}
	return query, args
}

type endpointChainGroupNode struct {
	CredentialGroup string `json:"credential_group"`
}

func endpointChainCredentialGroup(chain string) string {
	chain = strings.TrimSpace(chain)
	if chain == "" {
		return ""
	}

	var nodes []endpointChainGroupNode
	if err := json.Unmarshal([]byte(chain), &nodes); err == nil {
		for i := len(nodes) - 1; i >= 0; i-- {
			if nodes[i].CredentialGroup != "" {
				return nodes[i].CredentialGroup
			}
		}
	}

	parts := strings.Split(chain, " -> ")
	for i := len(parts) - 1; i >= 0; i-- {
		fields := strings.Split(parts[i], "::")
		if len(fields) >= 3 && strings.TrimSpace(fields[1]) != "" {
			return strings.TrimSpace(fields[1])
		}
	}
	return ""
}
