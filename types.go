package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/smtp"
)

// SenderConfig specifies the configuration to use for sending alerts.
type SenderConfig struct {
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Site specifies a site whose heartbeat has to be monitored.
type Site struct {
	Server          string          `json:"server"`
	Protocol        string          `json:"protocol"`
	HTTPConfig      HTTPConfig      `json:"http"`
	MySQLConfig     MySQLConfig     `json:"mysql"`
	SQLServerConfig SQLServerConfig `json:"sqlserver"`
	TimeoutSeconds  int64           `json:"timeoutSeconds"`
	Recipients      []string        `json:"recipients"`
}

// HTTPConfig specifies configuration for `http` and `https` services.
type HTTPConfig struct {
	Port       int             `json:"port"`
	URL        string          `json:"url"`
	Method     string          `json:"method"`
	Body       json.RawMessage `json:"body"`
	Accept403  bool            `json:"accept403"`
	VerifyCert bool            `json:"verifyCert"`
}

// MySQLConfig specifies configuration for MySQL services.
type MySQLConfig struct {
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// SQLServerConfig specifies configuration for SQL Server services.
type SQLServerConfig struct {
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Config holds the monitor's configuration.
type Config struct {
	Sender                 SenderConfig `json:"sender"`
	HeartbeatSeconds       int          `json:"heartbeatSeconds"`
	ResolverAddress        string       `json:"resolverAddress"`
	ResolverTimeoutSeconds int          `json:"resolverTimeoutSeconds"`
	Sites                  []Site       `json:"sites"`
}

// Monitor monitors the heartbeat of the servers specified in the
// configuration.
type Monitor struct {
	conf       *Config
	mailServer string
	resolver   *net.Resolver
}

//////////////////////////////////////////////////////////////////////

// loginAuth holds the username and password of the SMTP account.
type loginAuth struct {
	username string
	password string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", []byte{}, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		switch string(fromServer) {
		case "Username:":
			return []byte(a.username), nil
		case "Password:":
			return []byte(a.password), nil
		default:
			return nil, errors.New("unknown fromServer")
		}
	}
	return nil, nil
}

// LoginAuth answers an `smtp.Auth` compatible authenticator.
func LoginAuth(username, password string) smtp.Auth {
	return &loginAuth{username, password}
}
