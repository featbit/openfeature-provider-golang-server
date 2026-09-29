package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	featbitsdk "github.com/featbit/featbit-go-sdk"
	featbitprovider "github.com/featbit/openfeature-provider-golang-server"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
)

//go:embed watch.html
var watchPage []byte

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() (err error) {
	for _, name := range []string{
		"FEATBIT_ENV_SECRET", "FEATBIT_STREAMING_URL", "FEATBIT_EVENT_URL", "SERVICE_TARGETING_KEY",
	} {
		if os.Getenv(name) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}

	fbClient, err := featbitsdk.MakeCustomFBClient(
		os.Getenv("FEATBIT_ENV_SECRET"),
		os.Getenv("FEATBIT_STREAMING_URL"),
		os.Getenv("FEATBIT_EVENT_URL"),
		featbitsdk.FBConfig{StartWait: 10 * time.Second, LogLevel: featbitsdk.WARN},
	)
	// The SDK may return a client even when initialization fails.
	if fbClient != nil {
		defer func() { err = errors.Join(err, fbClient.Close()) }()
	}
	if err != nil {
		return err
	}
	provider, err := featbitprovider.NewProvider(fbClient)
	if err != nil {
		return err
	}

	api := isolated.NewAPI()
	defer func() { err = errors.Join(err, api.Shutdown(context.Background())) }()
	// This application requires ready flag data before accepting requests.
	if err := api.SetProviderAndWait(context.Background(), provider); err != nil {
		return err
	}

	app := &application{
		flags:        api.NewClient(),
		targetingKey: os.Getenv("SERVICE_TARGETING_KEY"),
		flagKey:      os.Getenv("FEATBIT_FLAG_KEY"),
	}
	if app.flagKey == "" {
		app.flagKey = "my-feature"
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write(watchPage); err != nil {
			log.Print("Could not write live view")
		}
	})
	mux.HandleFunc("GET /feature", app.feature)
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	log.Printf("Live view: http://%s/; GET /feature evaluates %s", addr, app.flagKey)

	var serveErr error
	select {
	case serveErr = <-serverErrors:
	case <-ctx.Done():
		stop()
	}

	// Finish active requests before the deferred OpenFeature and FeatBit cleanup.
	if err := server.Shutdown(context.Background()); err != nil {
		return errors.Join(serveErr, err)
	}
	if serveErr == nil {
		serveErr = <-serverErrors
	}
	if !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return nil
}

type application struct {
	flags        *openfeature.Client
	targetingKey string
	flagKey      string
}

func (app *application) feature(w http.ResponseWriter, r *http.Request) {
	// For per-user targeting, use the identity established by authentication middleware.
	evaluationContext := openfeature.NewEvaluationContext(app.targetingKey, map[string]any{
		"method": r.Method,
		"route":  "/feature",
	})
	details, err := app.flags.BooleanValueDetails(r.Context(), app.flagKey, false, evaluationContext)
	if err != nil {
		log.Print("Flag evaluation failed; using the default value")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	response := struct {
		FlagKey   string                `json:"flagKey"`
		Enabled   bool                  `json:"enabled"`
		Reason    openfeature.Reason    `json:"reason"`
		ErrorCode openfeature.ErrorCode `json:"errorCode,omitempty"`
	}{app.flagKey, details.Value, details.Reason, details.ErrorCode}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Print("Could not write response")
	}
}
