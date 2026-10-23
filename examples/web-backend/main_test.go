package main

import (
	"context"
	"errors"
	"licentivo-launch/pkg/launchclient"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServiceFailuresAreNotAuthenticationFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"network", &launchclient.TransportError{Cause: errors.New("private upstream details")}, 503},
		{"timeout", context.DeadlineExceeded, 504},
		{"unauthorized", &launchclient.APIError{Status: 401, Code: "UNAUTHENTICATED"}, 401},
		{"expired", &launchclient.APIError{Status: 403, Code: "ENTITLEMENT_EXPIRED"}, 403},
		{"pending", &launchclient.APIError{Status: 403, Code: "ENTITLEMENT_PENDING", RetryAfter: 3 * time.Second}, 409},
		{"SDK outage", &launchclient.RuntimeAPIError{Status: 503, Code: "NETWORK_UNAVAILABLE", Retryable: true}, 503},
		{"SDK device denial", &launchclient.RuntimeAPIError{Status: 403, Code: "DEVICE_LIMIT_EXCEEDED"}, 403},
		{"SDK feature denial", errFeatureDenied, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeFailure(response, test.err)
			if response.Code != test.status {
				t.Fatalf("status %d, want %d", response.Code, test.status)
			}
			if strings.Contains(response.Body.String(), "private upstream") {
				t.Fatal("leaked service details")
			}
			if test.name == "pending" && response.Header().Get("Retry-After") != "3" {
				t.Fatal("lost retry guidance")
			}
		})
	}
}
