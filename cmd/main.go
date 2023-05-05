package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/apex/log"
	"github.com/apex/log/handlers/logfmt"
	"github.com/apex/log/handlers/text"
	"github.com/cenkalti/backoff/v4"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"

	"github.com/networkteam/grazer"
)

func main() {
	app := &cli.App{
		Name:  "grazer",
		Usage: "Handle invalidates from Neos CMS and revalidation in Next.js",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "address",
				Value:   ":3100",
				Usage:   "Address for HTTP server to listen on",
				EnvVars: []string{"GZ_ADDRESS"},
			},
			&cli.StringFlag{
				Name:    "revalidate-token",
				Usage:   "A secret token to use for revalidation",
				EnvVars: []string{"GZ_REVALIDATE_TOKEN"},
			},
			&cli.StringFlag{
				Name:    "next-revalidate-url",
				Usage:   "The full URL to call to revalidate a page in Next.js",
				EnvVars: []string{"GZ_NEXT_REVALIDATE_URL"},
			},
			&cli.IntFlag{
				Name:    "revalidate-batch-size",
				Usage:   "The number of documents to send for revalidation in one batch to Next.js",
				Value:   1,
				EnvVars: []string{"GZ_REVALIDATE_BATCH_SIZE"},
			},
			&cli.DurationFlag{
				Name:    "revalidate-timeout",
				Usage:   "Timeout for revalidation requests",
				Value:   15 * time.Second,
				EnvVars: []string{"GZ_REVALIDATE_TIMEOUT"},
			},
			&cli.StringFlag{
				Name:    "neos-base-url",
				Usage:   "The base URL of the Neos CMS instance for fetching documents from the content API",
				EnvVars: []string{"GZ_NEOS_BASE_URL"},
			},
			&cli.StringFlag{
				Name:    "public-base-url",
				Usage:   "The publicly accessible base URL for sending correct proxy headers to Neos (for multi-site setups)",
				EnvVars: []string{"GZ_PUBLIC_BASE_URL"},
			},
			&cli.DurationFlag{
				Name:    "fetch-timeout",
				Usage:   "Timeout for fetching from the Neos content API",
				Value:   15 * time.Second,
				EnvVars: []string{"GZ_FETCH_TIMEOUT"},
			},
			&cli.DurationFlag{
				Name:    "initial-revalidate-delay",
				Usage:   "Delay before an initial revalidation of all pages, set to 0 to disable",
				Value:   15 * time.Second,
				EnvVars: []string{"GZ_INITIAL_REVALIDATE_DELAY"},
			},
			&cli.StringSliceFlag{
				Name:    "revalidate-schedule",
				Usage:   `Add a cron schedule to trigger revalidation of all pages (e.g. "@hourly", "@daily", "30 * * * *")`,
				EnvVars: []string{"GZ_REVALIDATE_SCHEDULE"},
			},
			&cli.BoolFlag{
				Name:    "verbose",
				Value:   false,
				Usage:   "Enable verbose logging",
				EnvVars: []string{"GZ_VERBOSE"},
			},
		},
		Before: func(c *cli.Context) error {
			if c.Bool("verbose") {
				log.SetLevel(log.DebugLevel)
			}
			setServerLogHandler(c)

			return nil
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
			defer cancel()

			revalidator := grazer.NewRevalidator(grazer.RevalidatorOpts{
				URL:             c.String("next-revalidate-url"),
				RevalidateToken: c.String("revalidate-token"),
				Timeout:         c.Duration("revalidate-timeout"),
			})

			fetcher := grazer.NewFetcher(grazer.FetcherOpts{
				Timeout:       c.Duration("fetch-timeout"),
				NeosBaseURL:   c.String("neos-base-url"),
				PublicBaseURL: c.String("public-base-url"),
			})

			h := grazer.NewHandler(grazer.HandlerOpts{
				Revalidator:         revalidator,
				Fetcher:             fetcher,
				RevalidateToken:     c.String("revalidate-token"),
				RevalidateBatchSize: c.Int("revalidate-batch-size"),
			})

			srv := &http.Server{
				Addr:    c.String("address"),
				Handler: h,
			}

			cr, err := createCron(c, h)
			if err != nil {
				return err
			}

			// Shutdown srv gracefully on signal and use wait group to wait for shutdown
			shutdownCtx, shutdownDone := context.WithCancel(context.Background())
			go func() {
				<-ctx.Done()
				log.Debug("Shutting down HTTP server...")
				if err := srv.Shutdown(shutdownCtx); err != nil {
					log.
						WithError(err).
						Error("Error shutting down server")
				}
				log.Debug("HTTP server shut down")

				log.Debug("Stopping cron...")
				cr.Stop()

				log.Debug("Waiting for controller to finish...")
				h.ShutdownAndWait()
				log.Debug("Controller finished")

				shutdownDone()
			}()

			if c.Duration("initial-revalidate-delay") > 0 {
				log.Debugf("Scheduling initial revalidation in %s", c.Duration("initial-revalidate-delay"))
				time.AfterFunc(c.Duration("initial-revalidate-delay"), func() {
					ctx := context.Background()
					err := backoff.Retry(func() error {
						log.
							WithField("component", "controller").
							Debug("Performing initial revalidate")

						err := h.FullRevalidate(ctx)
						if err != nil {
							log.
								WithError(err).
								Warn("Initial revalidation failed, retrying...")
						}
						return err
					}, backoff.NewExponentialBackOff())
					if err != nil {
						log.
							WithError(err).
							Error("Initial revalidation failed")
					}
				})
			}

			log.Infof("Listening on %s", c.String("address"))
			err = srv.ListenAndServe()
			if errors.Is(err, http.ErrServerClosed) {
				// expected error
			} else if err != nil {
				return fmt.Errorf("starting HTTP server: %w", err)
			}

			<-shutdownCtx.Done()
			log.Debug("Shutdown done")

			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func setServerLogHandler(c *cli.Context) {
	if isatty.IsTerminal(os.Stdout.Fd()) && !c.Bool("disable-ansi") {
		log.SetHandler(text.New(os.Stderr))
	} else {
		log.SetHandler(logfmt.New(os.Stderr))
	}
}
