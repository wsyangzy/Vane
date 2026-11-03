package codexproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/config"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/tracking"
)

func TestCodexProfileRecordsActualResourcesBeforeArchival(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"), BufferSize: 50, BatchSize: 5, FlushInterval: time.Hour, CleanupInterval: time.Hour, MaxRetry: 1})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { tracker.Close() })
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/first") {
					w.WriteHeader(503)
					io.WriteString(w, `{"error":{"code":"overloaded"}}`)
					return
				}
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n")
				} else {
					io.WriteString(w, `{"status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2}}`)
				}
			})
			h.usageTracker = tracker
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"test","stream":%t,"input":"client text"}`, streaming))))
			details, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 10})
			if err != nil || len(details) != 1 {
				t.Fatalf("missing request: %v %+v", err, details)
			}
			detail := details[0]
			var chain []chainNode
			if err := json.Unmarshal([]byte(detail.EndpointChain), &chain); err != nil {
				t.Fatal(err)
			}
			if rec.Code != 200 || detail.Status != "completed" || detail.Channel != "second" || detail.GroupName != "second" || detail.EndpointName != "second" || len(chain) != 2 || chain[0].StatusCode != 503 || chain[0].DurationMs == nil || chain[1].StatusCode != 200 {
				t.Fatalf("actual profile resources were lost: status=%d detail=%+v chain=%+v", rec.Code, detail, chain)
			}
			var progress routing.DispatchTrace
			if json.Unmarshal([]byte(detail.RouteSnapshot), &progress) != nil || progress.ProfileID != trace.ProfileID || progress.Attempts != 2 || !progress.Successful || progress.FinishedAt == nil {
				t.Fatalf("final profile outcome missing before archive: %s", detail.RouteSnapshot)
			}
			if !trace.Successful || trace.Attempts != 2 {
				t.Fatalf("final diagnostic record missing: %+v", trace)
			}
		})
	}
}

func TestCodexProfileStreamCompletenessMatchesArchivedOutcome(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"), BufferSize: 50, BatchSize: 5, FlushInterval: time.Hour, CleanupInterval: time.Hour, MaxRetry: 1})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { tracker.Close() })
			calls := 0
			raw := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial text\"}\n\n"
			if complete {
				raw += "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n"
			}
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if strings.HasPrefix(r.URL.Path, "/first") {
					w.WriteHeader(http.StatusForbidden)
					io.WriteString(w, `{"error":{"code":"AccessDenied"}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, raw)
			})
			h.usageTracker = tracker
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"test","stream":true}`)))
			if err := tracker.ForceFlush(); err != nil {
				t.Fatal(err)
			}
			details, err := tracker.QueryRequestDetails(context.Background(), &tracking.QueryOptions{Limit: 10})
			if err != nil || len(details) != 1 {
				t.Fatalf("missing archived request: %v %+v", err, details)
			}
			detail := details[0]
			wantOutcome, wantReason := tracking.StreamOutcomeIncomplete, "incomplete_stream"
			if complete {
				wantOutcome, wantReason = tracking.StreamOutcomeCompleted, ""
			}
			var archivedTrace routing.DispatchTrace
			if err := json.Unmarshal([]byte(detail.RouteSnapshot), &archivedTrace); err != nil {
				t.Fatal(err)
			}
			if detail.StreamOutcome != wantOutcome || detail.FailureReason != wantReason || archivedTrace.Successful != complete || trace.Successful != complete {
				t.Fatalf("request outcome contradicts dispatch: outcome=%s reason=%s trace=%+v", detail.StreamOutcome, detail.FailureReason, archivedTrace)
			}
			if rec.Code != 200 || rec.Body.String() != raw || calls != 2 || detail.Status != "completed" || detail.HTTPStatusCode == nil || *detail.HTTPStatusCode != 200 || !detail.HTTPStatusIsUpstream {
				t.Fatalf("response fidelity or attempt count changed: code=%d body=%q calls=%d detail=%+v", rec.Code, rec.Body.String(), calls, detail)
			}
		})
	}
}

func TestCodexProfileWithoutSendsDoesNotRecordLegacyActiveChannel(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, credentialRejected := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t/credential_rejected=%t", streaming, credentialRejected), func(t *testing.T) {
				tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"), BufferSize: 50, BatchSize: 5, FlushInterval: time.Hour, CleanupInterval: time.Hour, MaxRetry: 1})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { tracker.Close() })
				sends := 0
				h, runtime, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) { sends++ })
				h.usageTracker = tracker
				h.manager.activeChannel = &store.CodexChannelRecord{ID: 99, Name: "outside-profile"}
				profile := routing.DefaultProfile("codex")
				profile.ID, profile.Name, profile.IsDefault = 7, "current-profile", true
				profile.Members = []routing.RoutingMember{{ChannelID: 1, GroupID: 11, Enabled: true}}
				candidate := routing.DispatchCandidate{Provider: "codex", ChannelID: 1, GroupID: 11, EndpointID: "1", Protocol: "responses", Enabled: credentialRejected}
				runtime.Replace(routing.RuntimeConfig{Provider: "codex", Profiles: []routing.RoutingProfile{profile}, Candidates: []routing.DispatchCandidate{candidate}})
				if credentialRejected {
					h.manager.endpointsByChannel[1][0].AuthMode = subscription.ClientPassthrough
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"test","stream":%t,"input":"hello"}`, streaming))))
				if err := tracker.ForceFlush(); err != nil {
					t.Fatal(err)
				}
				details, err := tracker.QueryRequestDetails(context.Background(), &tracking.QueryOptions{Limit: 10})
				if err != nil || len(details) != 1 {
					t.Fatalf("missing request: %v %+v", err, details)
				}
				detail := details[0]
				var trace routing.DispatchTrace
				if err := json.Unmarshal([]byte(detail.RouteSnapshot), &trace); err != nil {
					t.Fatal(err)
				}
				if sends != 0 || trace.Attempts != 0 || trace.ProfileID != 7 || trace.FinishedAt == nil {
					t.Fatalf("expected a finished profile request without sends: sends=%d trace=%+v", sends, trace)
				}
				if detail.Channel != "" || detail.EndpointName != "" || detail.EndpointChain != "" {
					t.Fatalf("unsent request inherited resources: channel=%q endpoint=%q chain=%q", detail.Channel, detail.EndpointName, detail.EndpointChain)
				}
				if h.manager.GetActiveChannel().ID != 99 {
					t.Fatal("profile request changed global activation")
				}
			})
		}
	}
}

func TestCodexProfileTrackingOnlyRecordsNonStreamTargetWhenUsed(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", streaming), func(t *testing.T) {
			tracker, err := tracking.NewUsageTracker(&tracking.Config{Enabled: true, DatabasePath: filepath.Join(t.TempDir(), "usage.db"), BufferSize: 50, BatchSize: 5, FlushInterval: time.Hour, CleanupInterval: time.Hour, MaxRetry: 1})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { tracker.Close() })
			h, runtime, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n")
				} else {
					io.WriteString(w, `{"status":"completed","output":[]}`)
				}
			})
			h.usageTracker = tracker
			h.manager.activeChannel = &store.CodexChannelRecord{ID: 99, Name: "outside-profile"}
			profile := routing.DefaultProfile("codex")
			profile.ID, profile.Name, profile.IsDefault = 7, "current-profile", true
			profile.Members = []routing.RoutingMember{{ChannelID: 1, GroupID: 11, Enabled: true}}
			profile.Rules = []routing.RoutingRule{{ID: 1, Enabled: true, Kind: "non_stream", Mode: routing.ModePrefer, Targets: []routing.RouteTarget{{ChannelID: 2, GroupID: 22}}}}
			runtime.Replace(routing.RuntimeConfig{Provider: "codex", Profiles: []routing.RoutingProfile{profile}, Candidates: []routing.DispatchCandidate{
				{Provider: "codex", ChannelID: 1, GroupID: 11, EndpointID: "1", Protocol: "responses", Enabled: true},
				{Provider: "codex", ChannelID: 2, GroupID: 22, EndpointID: "2", Protocol: "responses", Enabled: true},
			}})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"test","stream":%t,"input":"hello"}`, streaming))))
			details, _, err := tracker.QueryRequestDetailsWithHotPool(context.Background(), &tracking.QueryOptions{Limit: 10})
			if err != nil || len(details) != 1 {
				t.Fatalf("missing request: %v %+v", err, details)
			}
			detail := details[0]
			wantChannel, wantDecision := "second", "non_stream"
			if streaming {
				wantChannel, wantDecision = "first", "default"
			}
			var trace routing.DispatchTrace
			if err := json.Unmarshal([]byte(detail.RouteSnapshot), &trace); err != nil {
				t.Fatal(err)
			}
			var chain []chainNode
			if err := json.Unmarshal([]byte(detail.EndpointChain), &chain); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusOK || detail.Channel != wantChannel || trace.Decision.Kind != wantDecision || trace.Attempts != 1 || len(chain) != 1 || chain[0].Channel != wantChannel {
				t.Fatalf("incorrect actual channel: status=%d channel=%q trace=%+v chain=%+v", rec.Code, detail.Channel, trace, chain)
			}
		})
	}
}

func newProfileCodexFixture(t *testing.T, upstream http.HandlerFunc, settlements ...func(routing.AttemptSettlement)) (*Handler, *routing.ProfileRuntime, *routing.DispatchTrace) {
	t.Helper()
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	channels := map[int64]*store.CodexChannelRecord{1: {ID: 1, Name: "first"}, 2: {ID: 2, Name: "second"}}
	manager := &EndpointManager{activeChannel: channels[1], channelsByID: channels, endpointsByChannel: map[int64][]*CodexEndpoint{}, unhealthyMap: map[int64]bool{}, failureDetails: map[int64]EndpointFailureDetail{}, logger: newTestLogger()}
	for i := int64(1); i <= 2; i++ {
		manager.endpointsByChannel[i] = []*CodexEndpoint{{ID: i, ChannelID: i, CredentialGroupID: i * 11, Name: channels[i].Name, CredentialGroupName: channels[i].Name, BaseURL: server.URL + "/" + channels[i].Name + "/v1", APIKey: fmt.Sprintf("key-%d", i), FailoverEnabled: false}}
	}
	p := routing.DefaultProfile("codex")
	p.ID, p.Name, p.IsDefault, p.Strategy = 1, "profile", true, routing.StrategyOrder
	p.Members = []routing.RoutingMember{{ChannelID: 1, GroupID: 11, Enabled: true}, {ChannelID: 2, GroupID: 22, Enabled: true}}
	cs := []routing.DispatchCandidate{{Provider: "codex", ChannelID: 1, GroupID: 11, EndpointID: "1", Protocol: "responses", Enabled: true}, {Provider: "codex", ChannelID: 2, GroupID: 22, EndpointID: "2", Protocol: "responses", Enabled: true}}
	var trace routing.DispatchTrace
	profiles := routing.NewProfileRuntime(routing.RuntimeConfig{Provider: "codex", Profiles: []routing.RoutingProfile{p}, Candidates: cs, PrepareCandidate: manager.PrepareDispatchCandidate, Supports: manager.SupportsDispatchCandidate, OnTrace: func(value routing.DispatchTrace) { trace = value }, OnSettlement: func(value routing.AttemptSettlement) {
		for _, settle := range settlements {
			settle(value)
		}
	}})
	// Unified sends must work even when both legacy account switches are off.
	pool := routing.NewPoolRuntime(routing.PoolConfig{Accounts: []routing.AccountState{{ChannelID: 1, GroupID: 11, Enabled: false}, {ChannelID: 2, GroupID: 22, Enabled: false}}}, nil, nil)
	pool.SetProfileRuntime(profiles)
	manager.SetPoolRuntime(pool)
	handler := NewHandler(manager, time.Second, config.RetryConfig{MaxAttempts: 3}, config.FailoverConfig{Enabled: true}, nil, newTestLogger(), nil)
	return handler, profiles, &trace
}

func TestCodexProfileRespectsGlobalAttemptCeiling(t *testing.T) {
	sends := 0
	h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":{"code":"overloaded"}}`)
	})
	h.UpdateConfig(config.RetryConfig{MaxTotalAttempts: 1}, config.FailoverConfig{Enabled: true})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/responses", strings.NewReader(`{"model":"test","stream":false,"input":"hello"}`)))
	if sends != 1 || trace.Attempts != 1 || trace.Successful {
		t.Fatalf("profile exceeded application budget: sends=%d trace=%+v", sends, trace)
	}
}

func TestCodexProfileActualStreamAndNonStreamFailoverKeepRequestAndActivation(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) {
			var mu sync.Mutex
			var paths []string
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				var body map[string]any
				_ = json.Unmarshal(raw, &body)
				if body["stream"] != streaming || body["input"] != "client text" || body["max_output_tokens"] != float64(123) || body["reasoning"].(map[string]any)["effort"] != "high" {
					t.Errorf("request fields changed: %s", raw)
				}
				if r.Header.Get("User-Agent") != "codex-client" || r.Header.Get("X-Relay-Key") != "" {
					t.Error("client identity lost or local auth leaked")
				}
				mu.Lock()
				paths = append(paths, r.URL.Path)
				mu.Unlock()
				if strings.HasPrefix(r.URL.Path, "/first") {
					w.WriteHeader(503)
					_, _ = io.WriteString(w, `{"error":{"code":"overloaded"}}`)
					return
				}
				if r.Header.Get("Authorization") != "Bearer key-2" {
					t.Error("fallback used wrong group credential")
				}
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"done\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n")
				} else {
					_, _ = io.WriteString(w, `{"id":"done","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2}}`)
				}
			})
			req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"original","stream":%t,"input":"client text","max_output_tokens":123,"reasoning":{"effort":"high"}}`, streaming)))
			req.Header.Set("User-Agent", "codex-client")
			req.Header.Set("X-Relay-Key", "application-key")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != 200 || !trace.Successful || trace.Attempts != 2 || !reflect.DeepEqual(paths, []string{"/first/v1/responses", "/second/v1/responses"}) {
				t.Fatalf("profile failover failed: %d %s paths=%v trace=%+v", rec.Code, rec.Body.String(), paths, trace)
			}
			if h.manager.GetActiveChannel().ID != 1 || h.manager.preferredEndpointID != 0 {
				t.Fatal("profile request changed global activation")
			}
		})
	}
}

func TestCodexProfileStartedStreamDoesNotReplayOrClaimSuccess(t *testing.T) {
	calls := 0
	h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"started\"}\n\n")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "data: {\"type\":\"error\",\"error\":{\"type\":\"server_error\",\"message\":\"interrupted\"}}\n\n")
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original","stream":true,"input":"text"}`)))
	if calls != 1 || !trace.ResponseStarted || trace.Successful || !strings.Contains(rec.Body.String(), "started") {
		t.Fatalf("started stream replayed/claimed success: calls=%d trace=%+v body=%s", calls, trace, rec.Body.String())
	}
}

func TestCodexProfileSealedStateAndResponseIDRemainWithTheirCandidate(t *testing.T) {
	var paths []string
	h, _, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = io.WriteString(w, `{"id":"owned-response","status":"completed","output":[{"type":"reasoning","encrypted_content":"sealed"}]}`)
	})
	for _, input := range []string{`{"model":"original","input":"first"}`, `{"model":"original","previous_response_id":"owned-response","input":[{"type":"function_call_output","call_id":"call-1","output":"result"}]}`} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(input)))
		if rec.Code != 200 {
			t.Fatalf("own state rejected: %d %s", rec.Code, rec.Body.String())
		}
	}
	if !reflect.DeepEqual(paths, []string{"/first/v1/responses", "/first/v1/responses"}) {
		t.Fatalf("state moved across candidate: %v", paths)
	}
}

func TestCodexProfileCompactionStateRoundTrip(t *testing.T) {
	for _, field := range []string{"object", "type"} {
		t.Run(field, func(t *testing.T) {
			var paths []string
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/compact") {
					_, _ = fmt.Fprintf(w, `{"id":"compact-owned","%s":"response.compaction","output":[{"type":"compaction","encrypted_content":"sealed-compact"}]}`, field)
					return
				}
				_, _ = io.WriteString(w, `{"id":"continued","status":"completed","output":[]}`)
			})
			for _, request := range []struct{ path, body string }{
				{"/v1/responses/compact", `{"model":"original","input":"compress history"}`},
				{"/v1/responses", `{"model":"original","input":[{"type":"compaction","encrypted_content":"sealed-compact"},{"role":"user","content":"continue"}]}`},
			} {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("POST", request.path, strings.NewReader(request.body)))
				if rec.Code != 200 || !trace.Successful || trace.Attempts != 1 {
					t.Fatalf("compaction round trip failed: %d %s trace=%+v", rec.Code, rec.Body.String(), trace)
				}
			}
			if !reflect.DeepEqual(paths, []string{"/first/v1/responses/compact", "/first/v1/responses"}) {
				t.Fatalf("compaction changed its backend: %v", paths)
			}
		})
	}
}

func TestCodexProfileCompletedItemsRegisterState(t *testing.T) {
	for _, item := range []string{
		`{"type":"reasoning","encrypted_content":"sealed-item"}`,
		`{"type":"function_call","call_id":"call-1","encrypted_function_args":"sealed-args"}`,
		`{"type":"function_call","call_id":"call-1","encrypted_function_args":["sealed-args"]}`,
	} {
		t.Run(item, func(t *testing.T) {
			calls := 0
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/event-stream")
				// Repeated response IDs must not fill the bounded state-key set.
				for i := 0; i < 260; i++ {
					_, _ = io.WriteString(w, "data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"stream-owned\"}}\n\n")
				}
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.output_item.done\",\"item\":%s}\n\n", item)
				_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"stream-owned\",\"status\":\"completed\"}}\n\n")
			})
			for _, body := range []string{
				`{"model":"original","stream":true,"input":"first"}`,
				fmt.Sprintf(`{"model":"original","stream":true,"previous_response_id":"stream-owned","input":[%s,{"type":"function_call_output","call_id":"call-1","output":"result"}]}`, item),
			} {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)))
				if rec.Code != 200 || !trace.Successful || trace.Attempts != 1 {
					t.Fatalf("completed item state rejected: %d %s trace=%+v", rec.Code, rec.Body.String(), trace)
				}
			}
			if calls != 2 {
				t.Fatalf("tool continuation replayed across candidates: calls=%d", calls)
			}
			foreign := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"original","stream":true,"input":[%s]}`, item)))
			foreign = foreign.WithContext(routing.WithCallerScope(foreign.Context(), "other-caller"))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, foreign)
			if rec.Code != 200 || calls != 3 {
				t.Fatalf("unknown history was locally rejected: %d calls=%d", rec.Code, calls)
			}
			keys := codexStateKeys(httptest.NewRequest("POST", "/v1/responses", nil), []byte(fmt.Sprintf(`{"input":[%s]}`, item)))
			owner := h.manager.PrepareDispatchCandidate(routing.DispatchCandidate{Provider: "codex", ChannelID: 1, GroupID: 11, EndpointID: "1", Protocol: "responses"}, "original", "").Identity()
			if got, ok := h.states.Preference(keys, ""); !ok || got != owner {
				t.Fatal("completed state lost issuing backend preference")
			}
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"original","stream":true,"input":[%s,{"type":"reasoning","encrypted_content":"unknown-state"}]}`, item))))
			if rec.Code != 200 || calls != 4 {
				t.Fatalf("mixed known and unknown history was locally rejected: %d calls=%d", rec.Code, calls)
			}
		})
	}
}

func TestCodexProfileUnfinishedStreamDoesNotRegisterState(t *testing.T) {
	for _, ending := range []string{
		"",
		"data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\"}}\n\n",
		"data: {\"type\":\"error\",\"error\":{\"type\":\"server_error\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n",
	} {
		t.Run(ending, func(t *testing.T) {
			calls := 0
			h, _, trace := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"started\"}\n\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"reasoning\",\"encrypted_content\":\"unfinished-state\"}}\n\n"+ending)
			})
			// Keep state ownership assertions independent of recovery retries.
			h.UpdateConfig(config.RetryConfig{MaxAttempts: 3, DisableRecoveryWait: true}, config.FailoverConfig{Enabled: true})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original","stream":true,"input":"first"}`)))
			if trace.Successful || !trace.ResponseStarted || calls != 1 {
				t.Fatalf("unfinished stream was replayed/claimed success: calls=%d trace=%+v", calls, trace)
			}
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"original","input":[{"type":"reasoning","encrypted_content":"unfinished-state"}]}`)))
			if calls != 2 || strings.Contains(rec.Body.String(), "state_origin_unknown") {
				t.Fatalf("unknown history was locally rejected: %d calls=%d body=%s", rec.Code, calls, rec.Body.String())
			}
		})
	}
}

func TestCodexProfileManagedAndPassthroughCredentialsNeedNoPoolKey(t *testing.T) {
	for _, managed := range []bool{true, false} {
		t.Run(fmt.Sprint(managed), func(t *testing.T) {
			h, _, trace := newProfileCodexFixture(t, func(http.ResponseWriter, *http.Request) { t.Fatal("official request used third party origin") })
			binding := subscription.Binding{ChannelID: 1, GroupID: 11, Version: 1, Enabled: true}
			if managed {
				binding.AccountID = "managed"
			}
			s := &poolSubscriptionStore{codexSubscriptionStore: &codexSubscriptionStore{}, bindings: []subscription.Binding{binding}}
			controller := subscription.NewController(s)
			controller.SetAccountResolver(func(context.Context, string) (string, string, error) {
				return "managed-token", "managed-workspace", nil
			}, func(context.Context, string) error { return nil })
			h.subscriptions = controller
			ep := h.manager.endpointsByChannel[1][0]
			ep.AuthMode, ep.BaseURL = subscription.ClientPassthrough, subscription.OfficialBaseURL
			ep.APIKey = ""
			calls := 0
			h.forwarder.client.Transport = codexSubscriptionTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				wantToken, wantAccount := "Bearer client-token", "client-workspace"
				if managed {
					wantToken, wantAccount = "Bearer managed-token", "managed-workspace"
				}
				if r.Header.Get("Authorization") != wantToken || r.Header.Get("ChatGPT-Account-ID") != wantAccount || r.Header.Get("X-Relay-Key") != "" || r.URL.Host != "chatgpt.com" {
					t.Fatalf("wrong profile credential egress: %s auth=%q account=%q", r.URL, r.Header.Get("Authorization"), r.Header.Get("ChatGPT-Account-ID"))
				}
				return codexSubscriptionResponseFor(200, `{"status":"completed","output":[]}`), nil
			})
			req := httptest.NewRequest("POST", "/backend-api/codex/responses", strings.NewReader(`{"model":"original","input":"hello"}`))
			req.Header.Set("Authorization", "Bearer client-token")
			req.Header.Set("ChatGPT-Account-ID", "client-workspace")
			req.Header.Set("X-Relay-Key", "application-key")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != 200 || calls != 1 || !trace.Successful {
				t.Fatalf("profile request still depended on pool auth: %d %s trace=%+v", rec.Code, rec.Body.String(), trace)
			}
		})
	}
}

func TestCodexUnifiedRuntimeWithoutDefaultNeverSendsToActiveChannel(t *testing.T) {
	sends := 0
	h, runtime, _ := newProfileCodexFixture(t, func(w http.ResponseWriter, r *http.Request) { sends++ })
	runtime.Replace(routing.RuntimeConfig{Provider: "codex"})
	for _, path := range []string{"/responses", "/responses/compact", "/backend-api/codex/models"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(map[bool]string{true: "GET", false: "POST"}[strings.HasSuffix(path, "/models")], path, strings.NewReader(`{"model":"test","stream":false,"input":"hello"}`)))
		if rec.Code < 400 || !strings.Contains(rec.Body.String(), "routing_profile_missing") {
			t.Fatalf("missing default was not reported: %d %s", rec.Code, rec.Body.String())
		}
	}
	if sends != 0 || h.manager.GetActiveChannel().ID != 1 {
		t.Fatalf("missing default fell back to active channel: sends=%d", sends)
	}
}
