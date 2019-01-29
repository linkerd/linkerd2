package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
)

var (
	port            = os.Getenv("PORT")
	_, amITheProxy  = os.LookupEnv("AM_I_THE_PROXY")
	hostname, _     = os.Hostname()
	defaultResponse = fmt.Sprintf("%s:%s", hostname, port)
)

func returnHostAndPortHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Got request [%v] returning [%s]", r, response())
	fmt.Fprintln(w, response())
}

func callOtherServiceHandler(w http.ResponseWriter, r *http.Request) {
	url := r.FormValue("url")
	log.Printf("Got request [%v] making HTTP call to [%s]", r, url)
	downstreamResp, err := http.Get(url)
	if err != nil {
		http.Error(w, err.Error(), 500)
	} else {
		body, err := ioutil.ReadAll(downstreamResp.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
		} else {
			response := fmt.Sprintf("me:[%s]downstream:[%s]", response(), strings.TrimSpace(string(body)))
			fmt.Fprintln(w, response)
		}
	}
}

func response() string {
	if amITheProxy {
		return "proxy"
	}

	return defaultResponse
}

func main() {
	fmt.Printf("Starting stub HTTP server on port [%s] will serve [%s] proxy [%t]", port, hostname, amITheProxy)

	http.HandleFunc("/", returnHostAndPortHandler)
	http.HandleFunc("/call", callOtherServiceHandler)
	err := http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
	if err != nil {
		panic(err)
	}
}
