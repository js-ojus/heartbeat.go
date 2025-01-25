package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"time"

	"go.uber.org/zap"
)

// checkHTTP makes a  HTTP(S) request to the given server, as per the
// given specification.
func (m *Monitor) checkHTTP(site *Site) error {
	var res *http.Response
	var err error

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	cl := &http.Client{
		Transport: tr,
		Timeout:   time.Duration(site.TimeoutMillis) * time.Millisecond,
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
		// Intentionally left blank.

	case res.StatusCode == 403:
		if !site.HTTPConfig.Accept403 {
			return fmt.Errorf("HTTP error : status : %d : %s", res.StatusCode, res.Status)
		}

	default:
		return fmt.Errorf("HTTP error : status : %d : %s", res.StatusCode, res.Status)
	}

	return nil
}

// checkHTTPx makes a  HTTP(S) request to the given server, as per the
// given specification.
func (m *Monitor) checkHTTPx(site *Site) error {
	writeError := func(err error) {
		zLog.Error(site.Protocol,
			zap.String("server", site.Server),
			zap.String("error", err.Error()))
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

	// Construct the request.
	var tDNSStart,
		tDNSDone,
		tConnectStart,
		tConnectDone,
		tTLSStart,
		tTLSDone,
		tFirstByte time.Time

	// Configure the request tracer.
	trace := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			tDNSStart = time.Now()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			tDNSDone = time.Now()
		},
		ConnectStart: func(network, addr string) {
			tConnectStart = time.Now()
		},
		ConnectDone: func(network, addr string, err error) {
			tConnectDone = time.Now()
		},
		TLSHandshakeStart: func() {
			tTLSStart = time.Now()
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			tTLSDone = time.Now()
		},
		GotFirstResponseByte: func() {
			tFirstByte = time.Now()
		},
	}

	// Configure the request.
	req, err := http.NewRequest(site.HTTPConfig.Method, fullURL, bytes.NewReader(site.HTTPConfig.Body))
	if err != nil {
		writeError(err)
		return err
	}
	_tr := httptrace.WithClientTrace(req.Context(), trace)
	req = req.WithContext(_tr)
	_trp := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: !site.HTTPConfig.VerifyCert},
		DisableKeepAlives: true,
	}

	// Make the request.
	start := time.Now()
	resp, err := _trp.RoundTrip(req)
	if err != nil {
		return fmt.Errorf("making request: %v", err)
	}
	defer resp.Body.Close()

	// Write metrics.
	tResolve := tDNSDone.Sub(tDNSStart).Milliseconds()
	tTotal := time.Since(start).Milliseconds()
	writeInfo := func() {
		zLog.Info(site.Protocol,
			zap.String("server", site.Server),
			zap.Int64("resolve", tResolve),
			zap.Int64("connect", tConnectDone.Sub(tConnectStart).Milliseconds()),
			zap.Int64("tls", tTLSDone.Sub(tTLSStart).Milliseconds()),
			zap.Int64("ttfb", tFirstByte.Sub(start).Milliseconds()),
			zap.Int64("total", tTotal))
	}
	writeError2 := func() {
		zLog.Error(site.Protocol,
			zap.String("server", site.Server),
			zap.Int("status", resp.StatusCode),
			zap.String("error", resp.Status))
	}

	switch {
	case resp.StatusCode == 200:
		// Intentionally left blank.

	case resp.StatusCode == 403:
		if !site.HTTPConfig.Accept403 {
			writeError2()
			return fmt.Errorf("HTTP error : status : %d : %s", resp.StatusCode, resp.Status)
		}

	default:
		writeError2()
		return fmt.Errorf("HTTP error : status : %d : %s", resp.StatusCode, resp.Status)
	}

	writeInfo()
	if tResolve >= int64(m.conf.ResolverTimeoutMillis) {
		sErr := fmt.Errorf("DNS resolution time limit exceeded: %d ms", tResolve)
		dErr := m.sendGmailAlert(site.Recipients, "dns", site.Server, sErr)
		if dErr != nil {
			zLog.Error("alert",
				zap.String("server", site.Server),
				zap.String("error", dErr.Error()))
		}
	}
	if tTotal >= site.TimeoutMillis {
		sErr := fmt.Errorf("total time limit exceeded: %d ms", tTotal)
		dErr := m.sendGmailAlert(site.Recipients, site.Protocol, site.Server, sErr)
		if dErr != nil {
			zLog.Error("alert",
				zap.String("server", site.Server),
				zap.String("error", dErr.Error()))
		}
	}
	return nil
}
