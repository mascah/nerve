package config

import _ "embed"

//go:embed config.scaffold.yaml
var scaffoldBytes []byte

// Scaffold returns the richly-commented .nerve/config.yaml scaffold that `nerve
// init` writes verbatim into a fresh project. Only `version:` + `project:` are
// active; every other section is shown as a commented example with the canonical
// keys. The returned slice is the embedded bytes — callers must not mutate it.
func Scaffold() []byte {
	return scaffoldBytes
}
