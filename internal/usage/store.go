package usage

import "context"

type Store interface {
	Save(ctx context.Context, record Record) error
}
