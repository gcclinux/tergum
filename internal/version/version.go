package version

import (
	_ "embed"
	"encoding/json"
)

//go:embed version.json
var versionJSON []byte

var (
	Version   = ""
	Build     = ""
	Commit    = "none"
	BuildDate = "unknown"
)

func init() {
	var data struct {
		Version string `json:"version"`
		Build   string `json:"build"`
	}
	if err := json.Unmarshal(versionJSON, &data); err == nil {
		if Version == "" {
			Version = data.Version
		}
		if Build == "" {
			Build = data.Build
		}
	}
}
