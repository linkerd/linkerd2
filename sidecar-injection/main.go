/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Note: the example only works with the code within the same release/branch.
package main

import (
		"fmt"
						// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"sync"
	"time"
	"net/http"
	"crypto/tls"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"
	)

// WaitSignal awaits for SIGINT or SIGTERM and closes the channel
func WaitSignal(stop chan struct{}) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	close(stop)
}

func main() {
	wh, _ := NewWebhook(WebhookParameters{Port: 8084})
	stop := make(chan struct{})
	go wh.Run(stop)
	WaitSignal(stop)
}

// Webhook implements a mutating webhook for automatic proxy injection.
type Webhook struct {
	mu                     sync.RWMutex
	sidecarTemplateVersion string

	healthCheckInterval time.Duration
	healthCheckFile     string

	server     *http.Server
	meshFile   string
	configFile string
	certFile   string
	keyFile    string
	cert       *tls.Certificate
}


// WebhookParameters configures parameters for the sidecar injection
type WebhookParameters struct {
	// ConfigFile is the path to the sidecar injection configuration file.
	ConfigFile string

	// MeshFile is the path to the mesh configuration file.
	MeshFile string

	// CertFile is the path to the x509 certificate for https.
	CertFile string

	// KeyFile is the path to the x509 private key matching `CertFile`.
	KeyFile string

	// Port is the webhook port, e.g. typically 443 for https.
	Port int

	// HealthCheckInterval configures how frequently the health check
	// file is updated. Value of zero disables the health check
	// update.
	HealthCheckInterval time.Duration

	// HealthCheckFile specifies the path to the health check file
	// that is periodically updated.
	HealthCheckFile string
}

// NewWebhook creates a new instance of a mutating webhook for automatic sidecar injection.
func NewWebhook(p WebhookParameters) (*Webhook, error) {
	duration, _ := time.ParseDuration("5s")
	wh := &Webhook{
		sidecarTemplateVersion: "",
		healthCheckInterval:    duration,
		healthCheckFile:        "",
		server: &http.Server{
			Addr: fmt.Sprintf(":%v", p.Port),
		},
		meshFile:   "",
		configFile: "",
		certFile:   "",
		keyFile:    "",
		cert:       nil,
	}
	// mtls disabled because apiserver webhook cert usage is still TBD.
	h := http.NewServeMux()
	h.HandleFunc("/inject", wh.serveInject)
	wh.server.Handler = h

	return wh, nil
}

// Run implements the webhook server
func (wh *Webhook) Run(stop <-chan struct{}) {
	go func() {
		if err := wh.server.ListenAndServe(); err != nil {
			fmt.Println("ListenAndServe for admission webhook returned error: %v", err)
		}
	}()

	defer wh.server.Close()  // nolint: errcheck

	for {
		select {
		case <-stop:
			return
		}
	}
}

func (wh *Webhook) serveInject(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		fmt.Println("no body found")
		http.Error(w, "no body found", http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		fmt.Println("contentType=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, want `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	fmt.Println("Webhook triggered!")
}

