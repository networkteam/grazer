package grazer

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"
	"sync"
	"time"

	"github.com/apex/log"
)

type HandlerOpts struct {
	RevalidateToken string

	Storage             *Storage
	Revalidator         *Revalidator
	Fetcher             *Fetcher
	RevalidateBatchSize int
}

type revalidateRequestDocument struct {
	RoutePath string `json:"routePath"`
}

type revalidateRequestBody struct {
	Documents []revalidateRequestDocument `json:"documents"`
}

type Handler struct {
	revalidateToken string

	ctrl *controller

	mux *http.ServeMux

	wg sync.WaitGroup
}

func NewHandler(opts HandlerOpts) *Handler {
	ctrl := newController(opts.Storage, opts.Revalidator, opts.Fetcher)
	if opts.RevalidateBatchSize == 0 {
		opts.RevalidateBatchSize = 1
	}
	ctrl.revalidateBatchSize = opts.RevalidateBatchSize

	mux := http.NewServeMux()
	h := &Handler{
		ctrl:            ctrl,
		revalidateToken: opts.RevalidateToken,
		mux:             mux,
	}

	mux.HandleFunc("/api/revalidate", h.handleRevalidate)
	mux.HandleFunc("/", h.catchAll)

	return h
}

func (h *Handler) handleRevalidate(w http.ResponseWriter, r *http.Request) {
	// Verify Authorization header matches the revalidate token
	authHeader := r.Header.Get("Authorization")
	if subtle.ConstantTimeCompare([]byte(authHeader), []byte(fmt.Sprintf("Bearer %s", h.revalidateToken))) != 1 {
		log.
			WithField("component", "http").
			Warn("invalid revalidate token")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Parse request body
	var body revalidateRequestBody
	err := json.NewDecoder(r.Body).Decode(&body)
	if err != nil {
		log.
			WithField("component", "http").
			WithError(err).Warn("decoding revalidate request body")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Start revalidation in background
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		log.
			WithField("component", "http").
			Info("revalidating invalidated documents")

		start := time.Now()

		ctx := context.Background()
		err := h.ctrl.revalidate(ctx, body.Documents)
		if err != nil {
			log.
				WithField("component", "http").
				WithError(err).
				Warn("revalidate failed")
		}

		log.
			WithField("component", "http").
			WithDuration(time.Since(start)).
			Info("revalidate finished")
	}()

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) catchAll(w http.ResponseWriter, r *http.Request) {
	dump, _ := httputil.DumpRequest(r, true)
	log.
		WithField("component", "http").
		WithField("url", r.URL).
		Debug(string(dump))

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) ShutdownAndWait() {
	h.ctrl.shutdownAndWait()
	h.wg.Wait()
}

func (h *Handler) InitialRevalidate(ctx context.Context) error {
	log.
		WithField("component", "controller").
		Debug("Performing initial revalidate")
	return h.ctrl.revalidate(ctx, nil)
}

type RevalidatorOpts struct {
	URL             string
	RevalidateToken string
	Timeout         time.Duration

	Transport http.RoundTripper
}

type Revalidator struct {
	url             string
	revalidateToken string

	client *http.Client
}

func NewRevalidator(opts RevalidatorOpts) *Revalidator {
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Second
	}

	return &Revalidator{
		url:             opts.URL,
		revalidateToken: opts.RevalidateToken,
		client: &http.Client{
			Timeout:   opts.Timeout,
			Transport: opts.Transport,
		},
	}
}

func (r *Revalidator) Revalidate(ctx context.Context, routePaths []string) error {
	documents := make([]revalidateRequestDocument, len(routePaths))
	for i, routePath := range routePaths {
		documents[i] = revalidateRequestDocument{
			RoutePath: routePath,
		}
	}

	var body bytes.Buffer
	err := json.NewEncoder(&body).Encode(revalidateRequestBody{
		Documents: documents,
	})
	if err != nil {
		return fmt.Errorf("encoding request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, &body)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.revalidateToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

type Fetcher struct {
	neosBaseURL string
	client      *http.Client
}

type FetcherOpts struct {
	Timeout     time.Duration
	NeosBaseURL string

	Transport http.RoundTripper
}

func NewFetcher(opts FetcherOpts) *Fetcher {
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}

	client := &http.Client{
		Timeout:   opts.Timeout,
		Transport: opts.Transport,
	}

	return &Fetcher{
		client:      client,
		neosBaseURL: opts.NeosBaseURL,
	}
}

type DocumentsItem struct {
	RoutePath string `json:"routePath"`
}

type DocumentsResponse struct {
	Documents []DocumentsItem `json:"documents"`
}

func (f *Fetcher) ListDocuments(ctx context.Context) (*DocumentsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/neos/content-api/documents", f.neosBaseURL), nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	var result DocumentsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}

type controller struct {
	mx sync.Mutex

	revalidateBatchSize int

	storage     *Storage
	revalidator *Revalidator
	fetcher     *Fetcher

	queue *queue
	sig   chan struct{}
	wg    sync.WaitGroup
}

func newController(storage *Storage, revalidator *Revalidator, fetcher *Fetcher) *controller {
	ctrl := &controller{
		revalidateBatchSize: 1,

		storage:     storage,
		revalidator: revalidator,
		fetcher:     fetcher,

		queue: newQueue(),
		sig:   make(chan struct{}),
	}

	ctrl.wg.Add(1)
	go ctrl.run()

	return ctrl
}

func (c *controller) revalidate(ctx context.Context, invalidatedDocuments []revalidateRequestDocument) error {
	c.mx.Lock()
	defer c.mx.Unlock()

	documentsResponse, err := c.fetcher.ListDocuments(ctx)
	if err != nil {
		return fmt.Errorf("listing documents: %w", err)
	}

	currentRoutePaths := make(map[string]struct{}, len(invalidatedDocuments)+len(documentsResponse.Documents))

	// Store route paths that were given for revalidation as known route paths
	invalidatedRoutePaths := make([]string, len(invalidatedDocuments))
	for i, document := range invalidatedDocuments {
		invalidatedRoutePaths[i] = document.RoutePath

		currentRoutePaths[document.RoutePath] = struct{}{}
	}

	// Store all route paths that were fetched as known route paths
	allRoutePaths := make([]string, len(documentsResponse.Documents))
	for i, document := range documentsResponse.Documents {
		allRoutePaths[i] = document.RoutePath

		currentRoutePaths[document.RoutePath] = struct{}{}
	}

	log.
		WithField("component", "controller").
		WithField("invalidatedRoutePaths", invalidatedRoutePaths).
		WithField("allRoutePaths", allRoutePaths).
		Debug("Enqueuing route paths")

	c.queue.enqueue(invalidatedRoutePaths, allRoutePaths)

	c.ensureProcessQueue()

	return nil
}

func (c *controller) shutdownAndWait() {
	close(c.sig)
	c.wg.Wait()
}

func (c *controller) run() {
	defer c.wg.Done()

	for {
		// Wait for signal to process the queue or a close of the channel
		_, ok := <-c.sig
		// The channel was closed
		if !ok {
			log.
				WithField("component", "controller").
				Debug("Returning from run loop")
			return
		}

		for {
			// Check if channel was closed while processing the queue
			select {
			case _, ok := <-c.sig:
				if !ok {
					log.
						WithField("component", "controller").
						Debug("Returning from run loop, stop processing the queue")
					return
				}
			default:
			}

			routePaths := c.queuePopBatch()
			if len(routePaths) == 0 {
				log.
					WithField("component", "controller").
					Debug("Queue is empty, stop processing")
				break
			}

			log.
				WithField("component", "controller").
				WithField("routePaths", routePaths).
				Info("Sending revalidate request")

			start := time.Now()

			ctx := context.Background()
			// TODO Add retry handling around this call
			err := c.revalidator.Revalidate(ctx, routePaths)
			if err != nil {
				log.
					WithField("component", "controller").
					WithField("routePaths", routePaths).
					WithError(err).
					Error("Revalidate failed")
			}

			log.
				WithField("component", "controller").
				WithField("routePaths", routePaths).
				WithDuration(time.Since(start)).
				Debug("Revalidate finished")
		}
	}
}

func (c *controller) ensureProcessQueue() {
	select {
	case c.sig <- struct{}{}:
		// Signal was sent
	default:
		// Signal was already sent
	}
}

func (c *controller) queuePopBatch() []string {
	var result []string
	for {
		s := c.queue.pop()
		if s == nil {
			break
		}
		result = append(result, *s)
		if len(result) == c.revalidateBatchSize {
			break
		}
	}
	return result
}

type Storage struct {
}

func NewStorage(dataPath string) (*Storage, error) {
	err := os.MkdirAll(dataPath, 0755)
	if err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	return &Storage{}, nil
}
