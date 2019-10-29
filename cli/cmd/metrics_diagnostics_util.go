package cmd

import (
	"io/ioutil"
	"net/http"
)

// GetResponse makes a http Get request to the passed url and returns the response/error
func GetResponse(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return bytes, nil
}
