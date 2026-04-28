package sqlite

import "github.com/dhruvmishra/codedojo/internal/modes/reviewer/mutate"

var _ mutate.LogStore = (*Store)(nil)
