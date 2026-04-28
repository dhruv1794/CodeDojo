package history

import (
	"context"
	"fmt"
)

type ScanCache interface {
	SaveNewcomerHistoryScan(ctx context.Context, repoURL, headSHA string, candidates []CommitCandidate) error
	GetNewcomerHistoryScan(ctx context.Context, repoURL, headSHA string) ([]CommitCandidate, error)
}

func ScanCached(ctx context.Context, cache ScanCache, repoURL, headSHA string, refresh bool, scan func(context.Context) ([]CommitCandidate, error)) ([]CommitCandidate, error) {
	if cache == nil {
		return scan(ctx)
	}
	if !refresh {
		cached, err := cache.GetNewcomerHistoryScan(ctx, repoURL, headSHA)
		if err == nil {
			return cached, nil
		}
	}
	candidates, err := scan(ctx)
	if err != nil {
		return nil, err
	}
	if err := cache.SaveNewcomerHistoryScan(ctx, repoURL, headSHA, candidates); err != nil {
		return nil, fmt.Errorf("cache newcomer history scan: %w", err)
	}
	return candidates, nil
}
