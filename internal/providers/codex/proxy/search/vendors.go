package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (c Config) Name() string {
	switch c.Vendor {
	case "brave":
		return "Brave Search"
	case "exa":
		return "Exa"
	case "searxng":
		return "SearXNG"
	default:
		return "Tavily"
	}
}

func buildCall(ctx context.Context, cfg Config, job work, include, exclude []string) (*http.Request, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	method := http.MethodPost
	var payload any
	start := ""
	if job.RecencyDays != nil && *job.RecencyDays > 0 {
		start = time.Now().UTC().AddDate(0, 0, -*job.RecencyDays).Format("2006-01-02")
	}
	switch cfg.Vendor {
	case "brave", "searxng":
		method = http.MethodGet
		q := u.Query()
		query := job.Q
		if len(include) > 0 {
			var sites []string
			for _, d := range include {
				sites = append(sites, "site:"+d)
			}
			query += " (" + strings.Join(sites, " OR ") + ")"
		}
		for _, d := range exclude {
			query += " -site:" + d
		}
		q.Set("q", query)
		if cfg.Vendor == "brave" {
			if job.images {
				u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/web/search") + "/images/search"
			}
			q.Set("count", strconv.Itoa(cfg.MaxResults))
			if start != "" {
				if job.images {
					return nil, errors.New("Brave 图片接口不支持精确日期筛选")
				}
				q.Set("freshness", start+"to"+time.Now().UTC().Format("2006-01-02"))
			}
		} else {
			q.Set("format", "json")
			if job.images {
				q.Set("categories", "images")
			}
			if start != "" {
				switch *job.RecencyDays {
				case 1:
					q.Set("time_range", "day")
				case 30, 31:
					q.Set("time_range", "month")
				case 365, 366:
					q.Set("time_range", "year")
				default:
					return nil, errors.New("SearXNG 时效仅支持 1、30、31、365、366 天")
				}
			}
		}
		u.RawQuery = q.Encode()
	case "exa":
		p := map[string]any{"query": job.Q, "numResults": cfg.MaxResults, "type": "auto", "contents": map[string]any{"text": map[string]any{"maxCharacters": 1500}}}
		// Some Exa-compatible services reject both lists in the same request.
		// The allowlist narrows upstream search; exclusions are still enforced
		// against every returned URL by runQuery.
		if len(include) > 0 {
			p["includeDomains"] = include
		} else if len(exclude) > 0 {
			p["excludeDomains"] = exclude
		}
		if start != "" {
			p["startPublishedDate"] = start + "T00:00:00Z"
		}
		payload = p
	default:
		p := map[string]any{"query": job.Q, "max_results": cfg.MaxResults, "search_depth": "basic", "include_domains": include, "exclude_domains": exclude, "include_images": job.images, "include_image_descriptions": job.images}
		if cfg.Vendor == "" || cfg.Vendor == "tavily" {
			p["include_usage"] = true
		}
		if start != "" {
			p["start_date"] = start
		}
		payload = p
	}
	var body []byte
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	r, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Accept", "application/json")
	if payload != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if cfg.APIKey != "" {
		switch cfg.Vendor {
		case "brave":
			r.Header.Set("X-Subscription-Token", cfg.APIKey)
		case "exa":
			r.Header.Set("X-API-Key", cfg.APIKey)
		default:
			r.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}
	}
	return r, nil
}

func decodeResults(vendor string, images bool, raw []byte) ([]Result, error) {
	var data struct {
		Results *[]struct {
			Title, URL, Content, Text, Description string
			ImageURL                               string               `json:"img_src"`
			Properties                             struct{ URL string } `json:"properties"`
		} `json:"results"`
		Web *struct {
			Results []struct{ Title, URL, Description string }
		} `json:"web"`
		Images []json.RawMessage `json:"images"`
		Error  json.RawMessage   `json:"error"`
	}
	if json.Unmarshal(raw, &data) != nil || len(data.Error) > 0 && string(data.Error) != "null" {
		return nil, fmt.Errorf("invalid search response")
	}
	var result []Result
	if vendor == "brave" && !images {
		if data.Web == nil {
			return nil, errors.New("missing web results")
		}
		for _, i := range data.Web.Results {
			result = append(result, Result{Title: i.Title, URL: i.URL, Snippet: i.Description})
		}
		return result, nil
	}
	if data.Results == nil {
		return nil, errors.New("missing results")
	}
	if images && (vendor == "tavily" || vendor == "") {
		for _, rawImage := range data.Images {
			var i struct{ URL, Description string }
			if json.Unmarshal(rawImage, &i) != nil {
				_ = json.Unmarshal(rawImage, &i.URL)
			}
			result = append(result, Result{Title: i.Description, URL: i.URL, Snippet: "Image search result: " + i.Description})
		}
		return result, nil
	}
	for _, i := range *data.Results {
		snippet := i.Content
		if vendor == "exa" {
			snippet = i.Text
		}
		if vendor == "brave" {
			snippet = i.Description
		}
		link := i.URL
		if images {
			if vendor == "brave" {
				link = i.Properties.URL
			} else {
				link = i.ImageURL
			}
			snippet = "Image search result: " + i.Title
		}
		result = append(result, Result{Title: i.Title, URL: link, Snippet: snippet})
	}
	return result, nil
}
