package app

func (a *App) GetSuspendedRequestsCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.proxyHandler != nil {
		return a.proxyHandler.GetSuspendedRequestsCount()
	}
	return 0
}

func (a *App) GetSuspendedEndpointCounts() map[string]int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.proxyHandler != nil {
		return a.proxyHandler.GetSuspendedEndpointCounts()
	}
	return map[string]int{}
}

func (a *App) GetManualRetryRequestsCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.proxyHandler != nil {
		return a.proxyHandler.GetManualRetryRequestsCount()
	}
	return 0
}

func (a *App) GetManualRetryChannelCounts() map[string]int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.proxyHandler != nil {
		return a.proxyHandler.GetManualRetryChannelCounts()
	}
	return map[string]int{}
}
