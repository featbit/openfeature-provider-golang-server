package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	featbitsdk "github.com/featbit/featbit-go-sdk/v2"
	featbitprovider "github.com/featbit/openfeature-provider-golang-server/v2"
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
		"FEATBIT_ENV_SECRET", "FEATBIT_STREAMING_URL", "FEATBIT_EVENT_URL",
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
	if err != nil {
		// The SDK may return a client even when initialization fails.
		if fbClient != nil {
			err = errors.Join(err, fbClient.Close())
		}
		return err
	}
	provider, err := featbitprovider.NewProvider(fbClient)
	if err != nil {
		return err
	}

	api := isolated.NewAPI()
	// OpenFeature shutdown also closes the FeatBit client.
	defer func() { err = errors.Join(err, api.Shutdown(context.Background())) }()
	// This application requires ready flag data before accepting requests.
	if err := api.SetProviderAndWait(context.Background(), provider); err != nil {
		return err
	}

	app := &application{
		flags:   api.NewClient(),
		flagKey: os.Getenv("FEATBIT_FLAG_KEY"),
	}
	if app.flagKey == "" {
		app.flagKey = "checkout-free-shipping"
	}
	addr := listenAddr()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write(watchPage); err != nil {
			log.Print("Could not write checkout page")
		}
	})
	mux.HandleFunc("POST /checkout/quote", app.checkoutQuote)
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
	log.Printf("Checkout demo: http://%s/; POST /checkout/quote evaluates %s", addr, app.flagKey)

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

func listenAddr() string {
	if addr := os.Getenv("LISTEN_ADDR"); addr != "" {
		return addr
	}
	return "127.0.0.1:1234"
}

type application struct {
	flags   *openfeature.Client
	flagKey string
}

const (
	freeShippingThresholdCents int64 = 5000
	standardShippingCents      int64 = 599
	maxSubtotalCents           int64 = 100_000_000
)

type quoteRequest struct {
	CustomerID    string `json:"customerId"`
	SubtotalCents *int64 `json:"subtotalCents"`
}

type quoteResponse struct {
	FlagKey       string                `json:"flagKey"`
	PromoApplied  bool                  `json:"promoApplied"`
	SubtotalCents int64                 `json:"subtotalCents"`
	ShippingCents int64                 `json:"shippingCents"`
	TotalCents    int64                 `json:"totalCents"`
	Reason        openfeature.Reason    `json:"reason"`
	ErrorCode     openfeature.ErrorCode `json:"errorCode,omitempty"`
}

func (app *application) checkoutQuote(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request quoteRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid checkout request"})
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "checkout request must contain one JSON object"})
		return
	}
	request.CustomerID = strings.TrimSpace(request.CustomerID)
	if request.CustomerID == "" || len(request.CustomerID) > 128 || request.SubtotalCents == nil ||
		*request.SubtotalCents < 0 || *request.SubtotalCents > maxSubtotalCents {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "customerId and a valid subtotalCents are required"})
		return
	}
	subtotalCents := *request.SubtotalCents

	evaluationContext := openfeature.NewEvaluationContext(request.CustomerID, map[string]any{
		"subtotalCents": subtotalCents,
	})
	details, err := app.flags.BooleanValueDetails(r.Context(), app.flagKey, false, evaluationContext)
	if err != nil {
		log.Print("Checkout flag evaluation failed; using standard shipping")
	}

	response := calculateCheckoutQuote(err == nil && details.Value, subtotalCents)
	response.FlagKey = app.flagKey
	response.Reason = details.Reason
	response.ErrorCode = details.ErrorCode
	writeJSON(w, http.StatusOK, response)
}

func calculateCheckoutQuote(promoEnabled bool, subtotalCents int64) quoteResponse {
	promoApplied := promoEnabled && subtotalCents >= freeShippingThresholdCents
	shippingCents := standardShippingCents
	if promoApplied {
		shippingCents = 0
	}
	return quoteResponse{
		PromoApplied:  promoApplied,
		SubtotalCents: subtotalCents,
		ShippingCents: shippingCents,
		TotalCents:    subtotalCents + shippingCents,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Print("Could not write response")
	}
}
