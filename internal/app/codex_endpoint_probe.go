package app

import (
	"context"
	"fmt"
	"github.com/wsyangzy/Vane/internal/platform/transport"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
	"net/http"
	"time"
)

func (a *App) ProbeCodexEndpoint(input codexservice.EndpointProbeInput) (codexservice.EndpointProbeResult, error) {
	a.ensureCodexProviderService()
	a.mu.RLock()
	service := a.codexEndpointService
	if service == nil || a.config == nil {
		a.mu.RUnlock()
		return codexservice.EndpointProbeResult{}, fmt.Errorf("Codex 端点服务未就绪")
	}
	cfg := *a.config
	a.mu.RUnlock()
	tr, err := transport.NewProvider(&cfg).Default()
	if err != nil {
		return codexservice.EndpointProbeResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := &http.Client{Transport: tr}
	defer client.CloseIdleConnections()
	return service.ProbeEndpoint(ctx, input, client)
}
