package docker

import (
	"context"
	"fmt"

	"github.com/dhruvmishra/codedojo/internal/sandbox"
)

type Driver struct{}

func (Driver) Start(ctx context.Context, spec sandbox.Spec) (sandbox.Session, error) {
	return nil, fmt.Errorf("docker sandbox driver is planned for Week 4")
}
