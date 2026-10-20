package api

import (
	"github.com/wsyangzy/Vane/internal/app"
)

type CacheService struct{ app *app.App }

func (s *CacheService) GetCacheStatus() (app.CacheStatus, error) {
	return s.app.GetCacheStatus()
}

func (s *CacheService) ClearCache(input app.ClearCacheInput) (app.CacheClearResult, error) {
	return s.app.ClearCache(input)
}
