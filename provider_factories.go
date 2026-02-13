package services

import (
	"github.com/goliatone/go-services/core"
	"github.com/goliatone/go-services/providers/github"
	"github.com/goliatone/go-services/providers/google/calendar"
	"github.com/goliatone/go-services/providers/google/docs"
	"github.com/goliatone/go-services/providers/google/drive"
	"github.com/goliatone/go-services/providers/google/gmail"
)

func GitHubProvider(cfg github.Config) (core.Provider, error) {
	return github.New(cfg)
}

func GmailProvider(cfg gmail.Config) (core.Provider, error) {
	return gmail.New(cfg)
}

func DriveProvider(cfg drive.Config) (core.Provider, error) {
	return drive.New(cfg)
}

func DocsProvider(cfg docs.Config) (core.Provider, error) {
	return docs.New(cfg)
}

func CalendarProvider(cfg calendar.Config) (core.Provider, error) {
	return calendar.New(cfg)
}
