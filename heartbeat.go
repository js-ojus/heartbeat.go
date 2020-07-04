package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/smtp"
	"os"
	"os/signal"
	"time"
)

// isServerUp makes a request to the given URL, and reports a non-nil
// error in case the server at the URL does not respond within the
// timeout duration.
func (m *Monitor) isServerUp(site *Site) error {
	switch site.Protocol {
	case "http", "https":
		return m.checkHTTP(site)

	case "mysql":
		return m.checkMySQL(site)

	default:
		return fmt.Errorf("unhandled protocol: %s", site.Protocol)
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
	fstr := "Subject: ALERT : Server down : %s\r\n" +
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
	l := len(m.conf.Sites)
	ch := make(chan bool)

	for _, site := range m.conf.Sites {
		go func(site Site, ch chan bool) {
			defer func() {
				ch <- true
			}()

			// Resolve the server, if it not an address.
			if ip := net.ParseIP(site.Server); ip == nil {
				err := m.resolveServer(site.Server)
				if err != nil {
					derr := m.sendAlert(site.Recipients, site.Server, err)
					if derr != nil {
						fmt.Printf("%s : ERROR : %v\n", time.Now().Format("2006-01-02 15:04:05"), derr)
					}
					return
				}
			}

			// Check for response.
			err := m.isServerUp(&site)
			if err != nil {
				derr := m.sendAlert(site.Recipients, site.Server, err)
				if derr != nil {
					fmt.Printf("%s : ERROR : %v\n", time.Now().Format("2006-01-02 15:04:05"), derr)
				}
			}
		}(site, ch)
	}

	for i := 0; i < l; i++ {
		<-ch
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

	// Set the resolver dialer.
	m.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Millisecond * time.Duration(10000),
			}
			return d.DialContext(ctx, "tcp", m.conf.ResolverAddress+":53")
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
	fmt.Print(".")
outer:
	for {
		select {
		case <-ticker.C:
			m.processSites()
			fmt.Print(".")

		case <-done:
			break outer
		}
	}
}
