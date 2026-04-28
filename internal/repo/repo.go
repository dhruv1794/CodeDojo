package repo

import gogit "github.com/go-git/go-git/v5"

type Repo struct {
	Path string
	Git  *gogit.Repository
}
