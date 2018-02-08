package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type MockHttpClient struct{}

func TestDashboardAvailability(t *testing.T) {
	t.Run("Returns true if dashboard HTTP request status code is 200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "Hello, client")
		}))
		defer ts.Close()

		dashboardAvailable, err := isDashboardAvailable(ts.Client(), ts.URL)
		if err != nil {
			t.Fatalf("Expected to not receive an error but got: %+v\n", err)
		}

		if !dashboardAvailable {
			t.Fatalf("Expected dashboard available to be true but got: %t", dashboardAvailable)
		}
	})

	t.Run("Returns true if dashboard HTTP request status code is 300", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusPermanentRedirect)
		}))
		defer ts.Close()

		dashboardAvailable, err := isDashboardAvailable(ts.Client(), ts.URL)
		if err != nil {
			t.Fatalf("Expected to not receive an error but got: %+v\n", err)
		}

		if !dashboardAvailable {
			t.Fatalf("Expected dashboard available to be true but got: %t", dashboardAvailable)
		}
	})
	t.Run("dashboardAvailable is false if dashboard HTTP request status code is 500", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		dashboardAvailable, err := isDashboardAvailable(ts.Client(), ts.URL)
		if err != nil {
			t.Fatalf("Expected to not receive an error but got: %+v\n", err)
		}

		if dashboardAvailable {
			t.Fatalf("Expected dashboard available to be true but got: %t", dashboardAvailable)
		}
	})
}
