// Package search adapts Codex's search tool to a separately configured search API.
// Native search remains in the proxy and never passes through this adapter.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const DefaultEndpoint = "https://api.tavily.com/search"

type Config struct {
	Mode       string
	Vendor     string
	Endpoint   string
	APIKey     string
	Timeout    time.Duration
	MaxResults int
}

func DefaultConfig() Config {
	return Config{Mode: "auto", Vendor: "tavily", Endpoint: DefaultEndpoint, Timeout: 30 * time.Second, MaxResults: 5}
}

func (c Config) Validate() error {
	if c.Mode != "auto" && c.Mode != "native" && c.Mode != "service" {
		return fmt.Errorf("搜索线路必须为自动选择、原生转发或搜索适配")
	}
	if c.Vendor != "" && c.Vendor != "tavily" && c.Vendor != "brave" && c.Vendor != "exa" && c.Vendor != "searxng" {
		return fmt.Errorf("不支持的搜索服务协议")
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("搜索服务地址必须为完整的 HTTP(S) 接口地址，不能包含凭证、查询参数或片段")
	}
	if (c.Vendor != "searxng" && strings.TrimSpace(c.APIKey) == "") || strings.ContainsAny(c.APIKey, " \t\r\n\x00") {
		return fmt.Errorf("请在 Codex 调度方案的搜索配置中填写 API Key")
	}
	if c.Timeout < time.Second || c.Timeout > 2*time.Minute || c.MaxResults < 1 || c.MaxResults > 20 {
		return fmt.Errorf("搜索超时必须在 1–120 秒之间，单次结果数必须在 1–20 之间")
	}
	return nil
}

type Request struct {
	Model    string                     `json:"model"`
	Commands map[string]json.RawMessage `json:"commands"`
	Settings struct {
		ExternalWebAccess *bool `json:"external_web_access"`
		Filters           struct {
			AllowedDomains []string `json:"allowed_domains"`
			BlockedDomains []string `json:"blocked_domains"`
		} `json:"filters"`
	} `json:"settings"`
}

type query struct {
	Q           string   `json:"q"`
	Domains     []string `json:"domains"`
	RecencyDays *int     `json:"recency_days"`
	Recency     *int     `json:"recency"`
}

func Parse(body []byte) (Request, error) {
	var req Request
	if json.Unmarshal(body, &req) != nil || strings.TrimSpace(req.Model) == "" || req.Commands == nil {
		return req, fmt.Errorf("搜索请求必须为包含 model 和 commands 的 JSON 对象")
	}
	return req, nil
}

type Result struct {
	Type    string `json:"type"`
	Domain  string `json:"domain"`
	RefID   string `json:"ref_id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type Reply struct {
	EncryptedOutput *string  `json:"encrypted_output"`
	Output          string   `json:"output"`
	Results         []Result `json:"results,omitempty"`
}

// Status is only set when the external service actually responded. Tool-facing
// HTTP 200 must not turn a failed external call into a completed tracking record.
type Failure struct {
	Reason string
	Detail string
	Status int
}

type Attempt struct {
	Sent   bool
	Status int
	Error  string
}

type Outcome struct {
	Reply    Reply
	Failure  *Failure
	Attempts []Attempt
}

type work struct {
	query
	images bool
}

// Run sends only extracted queries and filters, never client credentials,
// conversation context, or opaque native search state.
func Run(ctx context.Context, client *http.Client, cfg Config, req Request, referencePrefix string) Outcome {
	var out Outcome
	var notes []string
	var jobs []work
	unsupported := []string{}
	commandNames := make([]string, 0, len(req.Commands))
	for name := range req.Commands {
		commandNames = append(commandNames, name)
	}
	sort.Strings(commandNames)
	for _, name := range commandNames {
		raw := req.Commands[name]
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || bytes.Equal(bytes.TrimSpace(raw), []byte("[]")) {
			continue
		}
		if name != "search_query" && name != "image_query" {
			unsupported = append(unsupported, name)
			continue
		}
		var queries []query
		if json.Unmarshal(raw, &queries) != nil {
			out.Failure = &Failure{Reason: "search_request_invalid", Detail: "search_query 和 image_query 必须为查询数组"}
			break
		}
		for _, q := range queries {
			if q.RecencyDays != nil && q.Recency != nil && *q.RecencyDays != *q.Recency {
				out.Failure = &Failure{Reason: "search_request_invalid", Detail: "recency 与 recency_days 不能指定不同的时效天数"}
				break
			}
			if q.RecencyDays == nil {
				q.RecencyDays = q.Recency
			}
			if strings.TrimSpace(q.Q) == "" || len(q.Q) > 4096 || q.RecencyDays != nil && (*q.RecencyDays < 0 || *q.RecencyDays > 36500) {
				out.Failure = &Failure{Reason: "search_request_invalid", Detail: "查询不能为空、不能超过 4096 字节，时效天数必须为 0–36500"}
				break
			}
			jobs = append(jobs, work{query: q, images: name == "image_query"})
		}
	}
	sort.Strings(unsupported)
	if len(unsupported) > 0 {
		notes = append(notes, "Search adapter does not support these commands: "+strings.Join(unsupported, ", ")+". Use the native search route for opening pages, clicking links, finding text, and other native tools.")
	}
	if len(jobs) > 8 {
		out.Failure = &Failure{Reason: "search_request_invalid", Detail: "每次最多执行 8 条查询"}
	}
	if out.Failure == nil && len(jobs) == 0 {
		out.Failure = &Failure{Reason: "search_command_unsupported", Detail: "搜索适配支持 search_query 和 image_query；其他命令请使用原生线路"}
	}
	if out.Failure == nil && req.Settings.ExternalWebAccess != nil && !*req.Settings.ExternalWebAccess {
		out.Failure = &Failure{Reason: "search_cached_unsupported", Detail: "搜索适配没有官方缓存索引；请使用原生线路，或将客户端 web_search 设置为 live"}
	}
	if out.Failure == nil {
		if err := cfg.Validate(); err != nil {
			out.Failure = &Failure{Reason: "search_not_configured", Detail: err.Error()}
		}
	}
	if out.Failure == nil {
		for _, job := range jobs {
			results, attempt, failure := runQuery(ctx, client, cfg, req, job)
			out.Attempts = append(out.Attempts, attempt)
			if failure != nil {
				// Keep a real error response as the final error when another query
				// fails locally; individual tool notes still explain both failures.
				if out.Failure == nil || failure.Status >= 400 || out.Failure.Status < 400 {
					out.Failure = failure
				}
				notes = append(notes, "Search service failed: "+failure.Detail)
				if ctx.Err() != nil {
					break
				}
				continue
			}
			for _, result := range results {
				result.RefID = fmt.Sprintf("turn%ssearch%d", referencePrefix, len(out.Reply.Results))
				out.Reply.Results = append(out.Reply.Results, result)
				notes = append(notes, fmt.Sprintf("%s (%s)\n\ue200cite\ue202%s\ue201\n%s", result.Title, result.URL, result.RefID, result.Snippet))
			}
			if len(results) == 0 {
				notes = append(notes, "No results found for this query.")
			}
		}
	} else {
		notes = append(notes, "Search unavailable: "+out.Failure.Detail)
	}
	out.Reply.Output = strings.Join(notes, "\n\n")
	return out
}

func runQuery(ctx context.Context, client *http.Client, cfg Config, req Request, job work) ([]Result, Attempt, *Failure) {
	include := intersectDomains(job.Domains, req.Settings.Filters.AllowedDomains)
	if len(job.Domains) > 0 && len(req.Settings.Filters.AllowedDomains) > 0 && len(include) == 0 {
		return nil, Attempt{}, &Failure{Reason: "search_request_invalid", Detail: "查询域名与允许域名没有交集"}
	}
	if cfg.Vendor == "exa" && job.images {
		return nil, Attempt{}, &Failure{Reason: "search_command_unsupported", Detail: "Exa 适配支持网页查询；图片查询请选择 Tavily、Brave 或 SearXNG"}
	}
	if cfg.Vendor == "brave" && job.images && job.RecencyDays != nil && *job.RecencyDays > 0 {
		return nil, Attempt{}, &Failure{Reason: "search_command_unsupported", Detail: "Brave 图片接口不支持日期筛选，请移除时效限制或使用原生线路"}
	}
	if cfg.Vendor == "searxng" && job.RecencyDays != nil {
		switch *job.RecencyDays {
		case 0, 1, 30, 31, 365, 366:
		default:
			return nil, Attempt{}, &Failure{Reason: "search_command_unsupported", Detail: "SearXNG 时效仅支持 1、30/31、365/366 天，请选择这些时段或使用原生线路"}
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	call, err := buildCall(callCtx, cfg, job, include, req.Settings.Filters.BlockedDomains)
	if err != nil {
		return nil, Attempt{}, &Failure{Reason: "search_not_configured", Detail: "搜索服务地址或请求参数无效"}
	}
	resp, err := client.Do(call)
	if err != nil {
		detail := "搜索服务连接失败"
		if callCtx.Err() != nil {
			detail = "搜索服务请求取消或超时"
		}
		return nil, Attempt{Sent: true, Error: detail}, &Failure{Reason: "network_error", Detail: detail}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, Attempt{Sent: true, Status: resp.StatusCode}, &Failure{Reason: "response_read_error", Detail: "搜索服务响应读取失败或超过 2 MiB", Status: resp.StatusCode}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := string(raw)
		if cfg.APIKey != "" {
			detail = strings.ReplaceAll(detail, cfg.APIKey, "[REDACTED]")
		}
		if len(detail) > 4096 {
			detail = detail[:4096]
		}
		return nil, Attempt{Sent: true, Status: resp.StatusCode, Error: detail}, &Failure{Reason: "search_upstream_error", Detail: detail, Status: resp.StatusCode}
	}
	items, parseErr := decodeResults(cfg.Vendor, job.images, raw)
	if parseErr != nil {
		return nil, Attempt{Sent: true, Status: resp.StatusCode}, &Failure{Reason: "response_read_error", Detail: "搜索服务未返回有效的 " + cfg.Name() + " 搜索结果", Status: resp.StatusCode}
	}
	results := []Result{}
	add := func(title, link, snippet string) {
		u, err := url.Parse(link)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || !domainAllowed(u.Hostname(), include, req.Settings.Filters.BlockedDomains) {
			return
		}
		if len(results) < cfg.MaxResults {
			results = append(results, Result{Type: "text_result", Domain: u.Hostname(), Title: title, URL: link, Snippet: snippet})
		}
	}
	for _, item := range items {
		add(item.Title, item.URL, item.Snippet)
	}
	return results, Attempt{Sent: true, Status: resp.StatusCode}, nil
}

func matchesDomain(host, domain string) bool {
	host, domain = strings.ToLower(strings.TrimSuffix(host, ".")), strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
	return domain != "" && (host == domain || strings.HasSuffix(host, "."+domain))
}

func domainAllowed(host string, allowed, blocked []string) bool {
	for _, d := range blocked {
		if matchesDomain(host, d) {
			return false
		}
	}
	if len(allowed) == 0 {
		return true
	}
	for _, d := range allowed {
		if matchesDomain(host, d) {
			return true
		}
	}
	return false
}

func intersectDomains(queryDomains, allowed []string) []string {
	if len(queryDomains) == 0 {
		return allowed
	}
	if len(allowed) == 0 {
		return queryDomains
	}
	out := []string{}
	for _, q := range queryDomains {
		for _, a := range allowed {
			if matchesDomain(q, a) {
				out = append(out, q)
			} else if matchesDomain(a, q) {
				out = append(out, a)
			}
		}
	}
	return out
}
