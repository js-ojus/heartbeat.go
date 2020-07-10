package main

import (
	"bytes"
	"context"
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
		Timeout:   time.Duration(site.TimeoutSeconds) * time.Second,
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
		tConnectDone,
		tGotConn,
		tGotFirstResponseByte,
		tTLSHandshakeStart,
		tTLSHandshakeDone time.Time

	var dErr error
	trace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) { tDNSStart = time.Now() },
		DNSDone:  func(_ httptrace.DNSDoneInfo) { tDNSDone = time.Now() },
		ConnectStart: func(_, _ string) {
			if tDNSDone.IsZero() {
				// Connecting to IP address.
				tDNSDone = time.Now()
			}
		},
		ConnectDone: func(net, addr string, err error) {
			if err != nil {
				dErr = err
			}
			tConnectDone = time.Now()
		},
		GotConn:              func(_ httptrace.GotConnInfo) { tGotConn = time.Now() },
		GotFirstResponseByte: func() { tGotFirstResponseByte = time.Now() },
		TLSHandshakeStart:    func() { tTLSHandshakeStart = time.Now() },
		TLSHandshakeDone:     func(_ tls.ConnectionState, _ error) { tTLSHandshakeDone = time.Now() },
	}
	req, err := http.NewRequest(site.HTTPConfig.Method, fullURL, bytes.NewReader(site.HTTPConfig.Body))
	if err != nil {
		writeError(err)
		return err
	}
	req = req.WithContext(httptrace.WithClientTrace(context.Background(), trace))

	// Make the request.
	trp := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !site.HTTPConfig.VerifyCert},
	}
	cl := &http.Client{
		Transport: trp,
		Timeout:   time.Duration(site.TimeoutSeconds) * time.Second,
	}

	res, err := cl.Do(req)
	if err != nil {
		writeError(err)
		return err
	}
	if dErr != nil {
		writeError(err)
		return dErr
	}
	defer res.Body.Close()

	// Write metrics.
	tFinal := time.Now()
	if tDNSStart.IsZero() {
		tDNSStart = tDNSDone
	}
	tDNS := tDNSDone.Sub(tDNSStart).Milliseconds()
	tConnect := tConnectDone.Sub(tDNSDone).Milliseconds()
	tTLSHandshake := tTLSHandshakeDone.Sub(tTLSHandshakeStart).Milliseconds()
	tServer := tGotFirstResponseByte.Sub(tGotConn).Milliseconds()
	tTransfer := tFinal.Sub(tGotFirstResponseByte).Milliseconds()
	tTotal := tFinal.Sub(tDNSStart).Milliseconds()

	writeInfo := func() {
		zLog.Info(site.Protocol,
			zap.String("server", site.Server),
			zap.Int64("resolve", tDNS),
			zap.Int64("connect", tConnect),
			zap.Int64("tls", tTLSHandshake),
			zap.Int64("processing", tServer),
			zap.Int64("transfer", tTransfer),
			zap.Int64("total", tTotal))
	}
	writeError2 := func() {
		zLog.Error(site.Protocol,
			zap.String("server", site.Server),
			zap.Int("status", res.StatusCode),
			zap.String("error", res.Status))
	}

	switch {
	case res.StatusCode == 200:
		// Intentionally left blank.

	case res.StatusCode == 403:
		if !site.HTTPConfig.Accept403 {
			writeError2()
			return fmt.Errorf("HTTP error : status : %d : %s", res.StatusCode, res.Status)
		}

	default:
		writeError2()
		return fmt.Errorf("HTTP error : status : %d : %s", res.StatusCode, res.Status)
	}

	writeInfo()
	return nil
}
