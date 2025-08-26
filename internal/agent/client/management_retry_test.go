package client

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func Test_is5xxError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "500 error",
			err:  errors.New("status 500"),
			want: true,
		},
		{
			name: "502 error",
			err:  errors.New("status 502"),
			want: true,
		},
		{
			name: "503 error",
			err:  errors.New("status 503"),
			want: true,
		},
		{
			name: "504 error",
			err:  errors.New("status 504"),
			want: true,
		},
		{
			name: "400 error",
			err:  errors.New("status 400"),
			want: false,
		},
		{
			name: "404 error",
			err:  errors.New("status 404"),
			want: false,
		},
		{
			name: "non-status error",
			err:  errors.New("connection refused"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := is5xxError(tt.err)
			if got != tt.want {
				t.Errorf("is5xxError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_is5xxStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{
			name:       "500 status code",
			statusCode: 500,
			want:       true,
		},
		{
			name:       "502 status code",
			statusCode: 502,
			want:       true,
		},
		{
			name:       "503 status code",
			statusCode: 503,
			want:       true,
		},
		{
			name:       "504 status code",
			statusCode: 504,
			want:       true,
		},
		{
			name:       "599 status code",
			statusCode: 599,
			want:       true,
		},
		{
			name:       "400 status code",
			statusCode: 400,
			want:       false,
		},
		{
			name:       "404 status code",
			statusCode: 404,
			want:       false,
		},
		{
			name:       "200 status code",
			statusCode: 200,
			want:       false,
		},
		{
			name:       "600 status code",
			statusCode: 600,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := is5xxStatusCode(tt.statusCode)
			if got != tt.want {
				t.Errorf("is5xxStatusCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_retryWithInfiniteBackoff_Success(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	err := retryWithInfiniteBackoff(ctx, func() error {
		callCount++
		if callCount == 1 {
			return nil // Success on first try
		}
		return errors.New("should not be called again")
	})

	if err != nil {
		t.Errorf("retryWithInfiniteBackoff() error = %v, want nil", err)
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}
}

func Test_retryWithInfiniteBackoff_Non5xxError(t *testing.T) {
	ctx := context.Background()
	callCount := 0
	expectedErr := errors.New("status 404")

	err := retryWithInfiniteBackoff(ctx, func() error {
		callCount++
		return expectedErr
	})

	if err == nil {
		t.Errorf("retryWithInfiniteBackoff() error = nil, want error")
	}
	if err != expectedErr {
		t.Errorf("retryWithInfiniteBackoff() error = %v, want %v", err, expectedErr)
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (should not retry for non-5xx errors)", callCount)
	}
}

func Test_retryWithInfiniteBackoff_5xxError_WithTimeout(t *testing.T) {
	// Use a timeout longer than the base delay to allow at least one retry
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	callCount := 0

	err := retryWithInfiniteBackoff(ctx, func() error {
		callCount++
		return errors.New("status 500") // Always return 5xx error
	})

	if err == nil {
		t.Errorf("retryWithInfiniteBackoff() error = nil, want error")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("retryWithInfiniteBackoff() error = %v, want %v", err, context.DeadlineExceeded)
	}
	if callCount <= 1 {
		t.Errorf("callCount = %d, want > 1 (should have retried multiple times)", callCount)
	}
}

func Test_retryWithInfiniteBackoff_UsesBackoffLibrary(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	// Test that it works with backoff.Permanent for non-5xx errors
	err := retryWithInfiniteBackoff(ctx, func() error {
		callCount++
		if callCount == 1 {
			return errors.New("status 404") // Non-5xx error
		}
		return nil
	})

	if err == nil {
		t.Errorf("retryWithInfiniteBackoff() error = nil, want error")
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (should not retry for non-5xx errors)", callCount)
	}

	// Verify it contains the original error message
	if !strings.Contains(err.Error(), "status 404") {
		t.Errorf("Expected error to contain 'status 404', got %v", err)
	}
}

// Note: Integration tests for the management client would require more complex mocking
// of the HTTP client. The unit tests above verify the core retry logic functions.
