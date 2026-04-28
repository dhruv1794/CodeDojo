package mutate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const DefaultLogPath = ".codedojo/mutation-log.json"

type LogStore interface {
	SaveMutationLog(ctx context.Context, sessionID string, log MutationLog) error
	GetMutationLog(ctx context.Context, id string) (MutationLog, error)
	ListMutationLogs(ctx context.Context, sessionID string) ([]MutationLog, error)
}

func WriteMutationLog(repoPath string, log MutationLog) error {
	return WriteMutationLogFile(filepath.Join(repoPath, DefaultLogPath), log)
}

func WriteMutationLogFile(path string, log MutationLog) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create mutation log directory: %w", err)
	}
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mutation log: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write mutation log: %w", err)
	}
	return nil
}

func ReadMutationLogFile(path string) (MutationLog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MutationLog{}, fmt.Errorf("read mutation log: %w", err)
	}
	var log MutationLog
	if err := json.Unmarshal(data, &log); err != nil {
		return MutationLog{}, fmt.Errorf("unmarshal mutation log: %w", err)
	}
	return log, nil
}
