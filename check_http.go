package main

import (
	"bytes"
	"fmt"
	"net/http"
	"time"
)

// testHTTP makes a  HTTP(S) request to the given server, as per the
// given specification.
func (m *Monitor) testHTTP(site *Site) error {
	var res *http.Response
	var err error

	cl := &http.Client{
		Timeout: time.Duration(site.TimeoutSeconds) * time.Second,
	}

	// Construct the full URL.
	urlFmt := "%s://%s" // protocol://server
	urlParams := []interface{}{site.Protocol, site.Server}
	if site.HTTPConfig.Port != 0 {
		urlFmt += ":%d"
		urlParams = append(urlParams, site.HTTPConfig.Port)
	}
	if site.HTTPConfig.URL != "" {
		urlFmt += "/%s"
		urlParams = append(urlParams, site.HTTPConfig.URL)
	}
	fullURL := fmt.Sprintf(urlFmt, urlParams...)

	// Make the request based on the specified method.
	switch site.HTTPConfig.Method {
	case "HEAD":
		res, err = cl.Head(fullURL)

	case "GET":
		res, err = cl.Get(fullURL)

	case "POST":
		res, err = cl.Post(fullURL, "", bytes.NewReader(site.HTTPConfig.Body))
	}

	if err != nil {
		return fmt.Errorf("HTTP error : %w", err)
	}
	res.Body.Close()

	switch {
	case res.StatusCode == 200:
		return nil

	default:
		return fmt.Errorf("HTTP error : status : %d : %s", res.StatusCode, res.Status)
	}
}
