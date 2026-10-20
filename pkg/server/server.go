package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/heptiolabs/healthcheck"
	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/ziflex/lecho/v3"

	"github.com/zapier/kubechecks/pkg/checks"
	"github.com/zapier/kubechecks/pkg/container"
	"github.com/zapier/kubechecks/pkg/queue"
	"github.com/zapier/kubechecks/pkg/vcs"
)

const KubeChecksHooksPathPrefix = "/hooks"

type Server struct {
	ctr             container.Container
	processors      []checks.ProcessorEntry
	aiReviewChecker queue.AIReviewChecker
	queueManager    *queue.QueueManager
	echo            *echo.Echo
}

func NewServer(ctr container.Container, processors []checks.ProcessorEntry, aiReviewChecker queue.AIReviewChecker) *Server {
	// Create queue manager with configurable queue size
	queueSize := ctr.Config.MaxRepoWorkerQueueSize
	if queueSize <= 0 {
		queueSize = 100 // Fallback to default if not configured
	}

	queueManager := queue.NewQueueManager(
		queue.Config{QueueSize: queueSize},
		ProcessCheckEvent,
	)

	log.Info().
		Int("repo_worker_queue_size", queueSize).
		Msg("initialized repo worker queue manager")

	return &Server{
		ctr:             ctr,
		processors:      processors,
		aiReviewChecker: aiReviewChecker,
		queueManager:    queueManager,
	}
}

// Shutdown gracefully shuts down the HTTP server and queue workers
func (s *Server) Shutdown(ctx context.Context) error {
	log.Info().Msg("shutting down server")

	var httpErr, queueErr error

	// Shutdown HTTP server first (stop accepting new requests)
	if s.echo != nil {
		log.Info().Msg("shutting down HTTP server")
		if err := s.echo.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("HTTP server shutdown failed")
			httpErr = err
		}
	}

	// Then shutdown queue workers
	log.Info().Msg("shutting down queue workers")
	if err := s.queueManager.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("queue manager shutdown failed")
		queueErr = err
	}

	// Return first error if any
	if httpErr != nil {
		return httpErr
	}
	return queueErr
}

// Start initializes and starts the HTTP server (blocking)
func (s *Server) Start(ctx context.Context) error {
	if err := s.ensureWebhooks(ctx); err != nil {
		log.Warn().Err(err).Msg("failed to create webhooks")
	}

	s.echo = echo.New()
	s.echo.HideBanner = true
	s.echo.Logger = lecho.New(log.Logger)

	s.echo.Use(middleware.Recover())
	s.echo.Use(echoprometheus.NewMiddleware("kubechecks_echo"))

	// add routes
	health := healthcheck.NewHandler()
	s.echo.GET("/ready", echo.WrapHandler(health))
	s.echo.GET("/live", echo.WrapHandler(health))
	s.echo.GET("/metrics", echoprometheus.NewHandler())

	hooksGroup := s.echo.Group(s.hooksPrefix())

	ghHooks := NewVCSHookHandler(s.ctr, s.processors, s.aiReviewChecker, s.queueManager)
	ghHooks.AttachHandlers(hooksGroup)

	fmt.Println("Method\tPath")
	for _, r := range s.echo.Routes() {
		fmt.Printf("%s\t%s\n", r.Method, r.Path)
	}

	log.Info().Msg("starting HTTP server on :8080")
	if err := s.echo.Start(":8080"); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	return nil
}

func (s *Server) hooksPrefix() string {
	prefix := s.ctr.Config.UrlPrefix
	serverUrl, err := url.JoinPath("/", prefix, KubeChecksHooksPathPrefix)
	if err != nil {
		log.Warn().Err(err).Msg(":whatintarnation:")
	}

	return strings.TrimSuffix(serverUrl, "/")
}

// repoHostedOnVcs reports whether repo (a clone URL from the VcsToArgoMap) lives
// on the VCS this instance talks to. The map also holds Helm/OCI chart
// registries and, for ApplicationSets, un-rendered template URLs such as
// "{{ .chart.repoURL }}"; asking the VCS API about those is at best a 404 and,
// for the templated ones, a panic inside the GitHub client's parseRepo that
// takes the whole process down at boot (medgrupo, 2026-10-20).
func repoHostedOnVcs(repo, vcsBaseUrl string) bool {
	u, err := url.Parse(repo)
	if err != nil || u.Host == "" || strings.Contains(u.Path, "{{") {
		return false
	}
	base, err := url.Parse(vcsBaseUrl)
	if err != nil || base.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, base.Host)
}

func (s *Server) ensureWebhooks(ctx context.Context) error {
	if !s.ctr.Config.EnsureWebhooks {
		return nil
	}

	if !s.ctr.Config.MonitorAllApplications {
		return errors.New("must enable 'monitor-all-applications' to create webhooks")
	}

	urlBase := s.ctr.Config.WebhookUrlBase
	if urlBase == "" {
		return errors.New("must define 'webhook-url-base' to create webhooks")
	}

	log.Info().Msg("ensuring all webhooks are created correctly")

	vcsClient := s.ctr.VcsClient

	fullUrl, err := url.JoinPath(urlBase, s.hooksPrefix(), vcsClient.GetName(), "project")
	if err != nil {
		log.Warn().Str("urlBase", urlBase).Msg("failed to create a webhook url")
		return errors.Wrap(err, "failed to create a webhook url")
	}
	log.Info().Str("webhookUrl", fullUrl).Msg("webhook URL for this kubechecks instance")

	for _, repo := range s.ctr.VcsToArgoMap.GetVcsRepos() {
		if !repoHostedOnVcs(repo, s.ctr.Config.VcsBaseUrl) {
			log.Debug().Str("repo", repo).Msg("skipping webhook: repo is not hosted on the configured VCS")
			continue
		}

		wh, err := vcsClient.GetHookByUrl(ctx, repo, fullUrl)
		if err != nil && !errors.Is(err, vcs.ErrHookNotFound) {
			log.Error().Err(err).Msgf("failed to get hook for %s:", repo)
			continue
		}

		if wh == nil {
			if err = vcsClient.CreateHook(ctx, repo, fullUrl, s.ctr.Config.WebhookSecret); err != nil {
				log.Info().Err(err).Msgf("failed to create hook for %s:", repo)
			}
		}
	}

	return nil
}
