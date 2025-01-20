package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
)

const (
	// DefResolverTimeoutSeconds is used in case of no specification in
	// config.
	DefResolverTimeoutSeconds = 10
	// DefHTTPTimeoutSeconds is used in case of no specification in
	// config.
	DefHTTPTimeoutSeconds = 15
	// DefMySQLTimeoutSeconds is used in case of no specification in
	// config.
	DefMySQLTimeoutSeconds = 5
	// DefSQLServerTimeoutSeconds is used in case of no specification in
	// config.
	DefSQLServerTimeoutSeconds = 5
)

//

var zLog *zap.Logger

// isServerUp makes a request to the given URL, as per the specified
// protocol, and reports a non-nil error in case the server at the URL
// does not respond within the timeout duration.
func (m *Monitor) isServerUp(site *Site) error {
	switch site.Protocol {
	case "http", "https":
		if site.TimeoutSeconds == 0 {
			site.TimeoutSeconds = DefHTTPTimeoutSeconds
		}
		return m.checkHTTPx(site)

	case "mysql":
		if site.TimeoutSeconds == 0 {
			site.TimeoutSeconds = DefMySQLTimeoutSeconds
		}
		return m.checkMySQL(site)

	case "sqlserver":
		if site.TimeoutSeconds == 0 {
			site.TimeoutSeconds = DefSQLServerTimeoutSeconds
		}
		return m.checkSQLServer(site)

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
func (m *Monitor) sendAlert(recipients []string, server string, sErr error) error {
	auth := LoginAuth(m.conf.Sender.Username, m.conf.Sender.Password)
	fStr := "Subject: ALERT : Server not reachable : %s\r\n" +
		"\r\n" +
		"ERROR : Could not get heartbeat!\r\n" +
		"\r\n" +
		"Server : %s\r\n" +
		"Reason : %s\r\n"
	msg := fmt.Sprintf(fStr, server, server, sErr.Error())

	err := smtp.SendMail(m.mailServer, auth, m.conf.Sender.Username, recipients, []byte(msg))
	if err != nil {
		return err
	}

	return nil
}

// sendGMailAlert composes the alert message, and dispatches it using the SMTP
// configuration given in the configuration.
func (m *Monitor) sendGmailAlert(recipients []string, server string, sErr error) error {
	auth := smtp.PlainAuth("", m.conf.Sender.Username, m.conf.Sender.Password, m.conf.Sender.Server)

	// Construct email headers
	headers := make(map[string]string)
	headers["From"] = fmt.Sprintf("%s <%s>", "J S", m.conf.Sender.Username)
	headers["To"] = strings.Join(recipients, ",")
	headers["Subject"] = "ALERT : Server not reachable : " + server
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/html; charset=UTF-8"

	// Build message
	var message string
	for key, value := range headers {
		message += fmt.Sprintf("%s: %s\r\n", key, value)
	}
	message += "\r\n" + `
	<h3>ERROR : Could not get heartbeat!</h3>
	<p>Server : ` + server + `</p>
	<p>Reason : ` + sErr.Error() + `</p>
	`

	// Send email
	err := smtp.SendMail(
		m.mailServer,
		auth,
		m.conf.Sender.Username,
		recipients,
		[]byte(message),
	)

	return err
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
			trb := time.Now()
			if ip := net.ParseIP(site.Server); ip == nil {
				err := m.resolveServer(site.Server)
				if err != nil {
					zLog.Error("dns",
						zap.String("server", site.Server),
						zap.String("error", err.Error()))

					dErr := m.sendGmailAlert(site.Recipients, site.Server, err)
					if dErr != nil {
						zLog.Error("alert",
							zap.String("server", site.Server),
							zap.String("error", dErr.Error()))
					}

					return
				}

				tre := time.Now()
				dur := tre.Sub(trb)
				zLog.Info("dns",
					zap.String("server", site.Server),
					zap.Int64("ms", dur.Milliseconds()))
			}

			// Check for response, as per the specified protocol.
			if err := m.isServerUp(&site); err != nil {
				dErr := m.sendGmailAlert(site.Recipients, site.Server, err)
				if dErr != nil {
					zLog.Error("alert",
						zap.String("server", site.Server),
						zap.String("error", dErr.Error()))
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
	fVersion := flag.Bool("v", false, "print version information")
	flag.Parse()
	if *fVersion {
		info, _ := debug.ReadBuildInfo()
		fmt.Printf("Go Version      : %s\n", info.GoVersion)
		fmt.Printf("Program Version : %s\n", info.Main.Version)
		fmt.Println()
		return
	}

	var err error

	zCfg := []byte(`{
		"level": "info",
		"encoding": "json",
		"outputPaths": ["log/` +
		"hb.log." + time.Now().Format("2006-01-02_15-04-05") +
		`"],
		"errorOutputPaths": ["stderr"],
		"encoderConfig": {
		    "messageKey": "type",
		    "levelKey": "level",
		    "levelEncoder": "capital",
		    "timeKey": "at",
		    "timeEncoder": "iso8601"
		}
	}`)

	// Initialise logger.
	var cfg zap.Config
	if err = json.Unmarshal(zCfg, &cfg); err != nil {
		fmt.Printf("!! Unable to initialize logging : %s\n", err.Error())
		return
	}
	zLog, err = cfg.Build()
	if err != nil {
		fmt.Printf("!! Unable to initialise logger : %s\n", err.Error())
		return
	}
	defer zLog.Sync()

	buf, err := os.ReadFile("config.json")
	if err != nil {
		fmt.Printf("!! Unable to read `config.json` : %s\n", err.Error())
		return
	}

	// Read the configuration.
	m := &Monitor{
		conf: &Config{},
	}
	err = json.Unmarshal(buf, m.conf)
	if err != nil {
		fmt.Printf("!! Corrupt configuration JSON : %s\n", err.Error())
		return
	}
	if m.conf.ResolverTimeoutSeconds == 0 {
		m.conf.ResolverTimeoutSeconds = DefResolverTimeoutSeconds
	}

	// Set the outgoing server and sender's name.
	m.mailServer = fmt.Sprintf("%s:%d", m.conf.Sender.Server, m.conf.Sender.Port)

	// Set the resolver dialer.
	m.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Second * time.Duration(m.conf.ResolverTimeoutSeconds),
			}
			return d.DialContext(ctx, "tcp", m.conf.ResolverAddress+":53")
		},
	}

	// Main loop.
	done := make(chan struct{})
	go func(ch chan struct{}) {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
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
