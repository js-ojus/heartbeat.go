package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"os/signal"
	"time"
)

// isSiteUp makes a request to the given URL, and reports a non-nil
// error in case the server at the URL does not respond within the
// timeout duration.
func (m *Monitor) isSiteUp(url string, timeout time.Duration) error {
	var res *http.Response
	var err error
	cl := &http.Client{
		Timeout: timeout * time.Second,
	}
	if m.conf.RequestHeadOnly {
		res, err = cl.Head(url)
	} else {
		res, err = cl.Get(url)
	}
	if err != nil {
		return fmt.Errorf("HTTP error : %w", err)
	}

	switch {
	case res.StatusCode == 200:
		return nil

	default:
		return fmt.Errorf("HTTP error : status : %d : %s", res.StatusCode, res.Status)
	}
}

// resolveServer uses Go's native name resolver with the given DNS
// server, to get addresses for the specified host.
func (m *Monitor) resolveServer(host string) error {
	_, err := m.resolver.LookupHost(context.Background(), host)
	if err != nil {
		return err
	}

	return nil
}

// sendAlert composes the alert message, and dispatches it using the
// SMTP configuration given in the configuration.
func (m *Monitor) sendAlert(recipients []string, server string, serr error) error {
	auth := LoginAuth(m.conf.Sender.Username, m.conf.Sender.Password)
	fstr := "From: ISB Heartbeat Monitor\r\n" +
		"Subject: ALERT : Server down : %s\r\n" +
		"\r\n" +
		"ERROR : Could not get heartbeat!\r\n" +
		"\r\n" +
		"Server : %s\r\n" +
		"Reason : %s\r\n"
	msg := fmt.Sprintf(fstr, server, server, serr.Error())

	err := smtp.SendMail(m.mailServer, auth, m.conf.Sender.Username, recipients, []byte(msg))
	if err != nil {
		return err
	}

	return nil
}

// processSites is the main loop of the heartbeat checker.
func (m *Monitor) processSites() {
	for _, site := range m.conf.Sites {
		// Resolve the URL, if it not an address.
		if !site.IsAddress {
			err := m.resolveServer(site.URL)
			if err != nil {
				derr := m.sendAlert(site.Recipients, site.URL, err)
				if derr != nil {
					fmt.Printf("%s : ERROR : %v\n", time.Now().Format("2006-01-02 15:04:05"), derr)
				}
				continue
			}
		}

		// Check for response.
		err := m.isSiteUp(site.URL, time.Duration(site.TimeoutSeconds)*time.Second)
		if err != nil {
			derr := m.sendAlert(site.Recipients, site.URL, err)
			if derr != nil {
				fmt.Printf("%s : ERROR : %v\n", time.Now().Format("2006-01-02 15:04:05"), derr)
			}
			continue
		}
	}
}

// main is the driver.
func main() {
	buf, err := ioutil.ReadFile("config.json")
	if err != nil {
		fmt.Printf("!! Unable to read `config.json` : %v\n", err)
		return
	}

	// Read the configuration.
	m := &Monitor{
		conf: &Config{},
	}
	err = json.Unmarshal(buf, m.conf)
	if err != nil {
		fmt.Printf("!! Corrupt configuration JSON : %v\n", err)
		return
	}

	// Set the outgoing server and sender's name.
	m.mailServer = fmt.Sprintf("%s:%d", m.conf.Sender.Server, m.conf.Sender.Port)
	m.senderName = fmt.Sprintf("%s <%s>", m.conf.Sender.FromName, m.conf.Sender.FromAddress)

	// Set the resolver dialer.
	m.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := net.Dial(network, addr)
			if err != nil {
				return nil, err
			}
			return conn, nil
		},
	}

	// Main loop.
	done := make(chan struct{})
	go func(ch chan struct{}) {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, os.Kill)
		<-sig
		fmt.Println("Shutting down heartbeat monitor ...")

		close(ch)
	}(done)

	ticker := time.NewTicker(time.Duration(m.conf.HeartbeatSeconds) * time.Second)
	defer ticker.Stop()

	fmt.Println("Starting heartbeat monitor ...")
	m.processSites()
outer:
	for {
		select {
		case <-ticker.C:
			m.processSites()

		case <-done:
			break outer
		}
	}
}
