package main

import (
	"context"
	"github.com/haimgel/kan-brewer/internal/config"
	"github.com/haimgel/kan-brewer/internal/sync"
	"github.com/urfave/cli/v3"
	"log/slog"
	"os"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Config{}
	version := config.NewVersion()

	cmd := &cli.Command{
		Name:    "kan-brewer",
		Usage:   "Create Kanister ActionSets based on Namespace and Pvc annotations",
		Version: version.Release,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "namespace",
				Aliases:     []string{"n"},
				Usage:       "namespace to create ActionSets in",
				Value:       "kanister",
				Destination: &cfg.ActionSetNamespace,
			},
			&cli.IntFlag{
				Name:        "keep-successful",
				Aliases:     []string{"k"},
				Usage:       "number of successful previous ActionSets to keep",
				Value:       3,
				Destination: &cfg.KeepCompletedActionSets,
			},
		},
		Action: func(context.Context, *cli.Command) error {
			logger.Info("Starting KanBrewer", "version", version.Release, "commit", version.Commit, "date", version.Date)
			s, err := sync.NewSynchronizer(cfg, logger)
			if err != nil {
				return err
			}
			err = s.Process()
			if err != nil {
				return err
			}
			logger.Info("Shutting down")
			return nil
		},
	}

	if err := cmd.Run(context.TODO(), os.Args); err != nil {
		logger.Error("Fatal error", "error", err)
		os.Exit(1)
	}
}
