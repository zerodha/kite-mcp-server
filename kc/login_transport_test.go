package kc

import (
	"context"
	"io"
	"net/http"
	"testing"
	"testing/synctest"
	"time"
)

type loginRoundTripFunc func(*http.Request) (*http.Response, error)

func (f loginRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type loginContextBody struct{ ctx context.Context }

func (b loginContextBody) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }
func (loginContextBody) Close() error               { return nil }

func TestLoginExchangePreservesBothTimeoutsThroughResponseBody(t *testing.T) {
	for _, phase := range []string{"headers", "body"} {
		for _, limiting := range []string{"client", "review"} {
			t.Run(phase+"/"+limiting, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					clientTimeout, reviewTimeout := time.Second, 5*time.Minute
					if limiting == "review" {
						clientTimeout, reviewTimeout = 5*time.Minute, time.Second
					}
					ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
					defer cancel()
					transport := &loginResponseTransport{ctx: ctx, base: loginRoundTripFunc(func(req *http.Request) (*http.Response, error) {
						if phase == "headers" {
							<-req.Context().Done()
							return nil, req.Context().Err()
						}
						return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: loginContextBody{req.Context()}, Request: req}, nil
					})}
					client := &http.Client{Timeout: clientTimeout, Transport: transport}
					start := time.Now()
					response, err := client.Get("https://kite.example/session/token")
					if err == nil {
						_, err = io.ReadAll(response.Body)
						if closeErr := response.Body.Close(); closeErr != nil {
							t.Fatal(closeErr)
						}
					}
					if err == nil {
						t.Fatal("stalled login did not time out")
					}
					if elapsed := time.Since(start); elapsed != time.Second {
						t.Fatalf("login ignored shorter timeout: %s", elapsed)
					}
				})
			})
		}
	}
}
