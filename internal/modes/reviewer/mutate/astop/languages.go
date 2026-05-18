// SPDX-License-Identifier: MIT

package astop

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/rust"
	tsx "github.com/smacker/go-tree-sitter/typescript/tsx"
)

func getRustLanguage() *sitter.Language {
	return rust.GetLanguage()
}

func getJSLanguage() *sitter.Language {
	return javascript.GetLanguage()
}

func getTSLanguage() *sitter.Language {
	return tsx.GetLanguage()
}
