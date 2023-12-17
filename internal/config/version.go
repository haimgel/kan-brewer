package config

import (
	"fmt"
)

// These variables are set at build time: go build --ldflags "-X github.com/haimgel/kan-brewer/internal/config.release=1.0.0 ..."
// nolint: gochecknoglobals
var (
	release = "dev"
	commit  = ""
	date    = ""
)

type Version struct {
	Release string
	Commit  string
	Date    string
}

func NewVersion() Version {
	return Version{
		Release: release,
		Commit:  commit,
		Date:    date,
	}
}

func (v *Version) AppNameAndVersion(details bool) string {
	result := fmt.Sprintf("KanBrewer %s", v.Release)
	if details && v.Commit != "" {
		result = fmt.Sprintf("%s\nGit commit: %s", result, v.Commit)
	}
	if details && v.Date != "" {
		result = fmt.Sprintf("%s\nBuilt at: %s", result, v.Date)
	}
	return result
}
