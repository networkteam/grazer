package main

import (
	"context"
	"fmt"

	"github.com/apex/log"
	"github.com/cenkalti/backoff/v4"
	"github.com/robfig/cron/v3"
	"github.com/urfave/cli/v2"

	"github.com/networkteam/grazer"
)

func createCron(c *cli.Context, h *grazer.Handler) (*cron.Cron, error) {
	logger := cronLogger{log.Log}
	cr := cron.New(cron.WithChain(
		cron.Recover(logger),
		cron.SkipIfStillRunning(logger),
	))

	if revalidateSchedules := c.StringSlice("revalidate-schedule"); len(revalidateSchedules) > 0 {
		for _, schedule := range revalidateSchedules {
			_, err := cr.AddFunc(schedule, func() {
				ctx := context.Background()
				err := backoff.Retry(func() error {
					log.
						WithField("component", "controller").
						Debug("Performing scheduled revalidate")

					err := h.FullRevalidate(ctx)
					if err != nil {
						log.
							WithError(err).
							Warn("Scheduled revalidation failed, retrying...")
					}
					return err
				}, backoff.NewExponentialBackOff())
				if err != nil {
					log.
						WithError(err).
						Error("Scheduled revalidation failed")
				}
			})
			if err != nil {
				return nil, fmt.Errorf("invalid revalidate schedule: %w", err)
			}
		}
	}
	cr.Start()

	return cr, nil
}

type cronLogger struct{ log log.Interface }

func (c cronLogger) Info(msg string, keysAndValues ...interface{}) {
	c.createEntry(keysAndValues).Info(msg)
}

func (c cronLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	c.createEntry(keysAndValues).WithError(err).Error(msg)
}

func (c cronLogger) createEntry(keysAndValues []interface{}) *log.Entry {
	entry := c.log.WithField("component", "cron")
	for i := 0; i < len(keysAndValues); i += 2 {
		entry = entry.WithField(fmt.Sprint(keysAndValues[i]), keysAndValues[i+1])
	}
	return entry
}
