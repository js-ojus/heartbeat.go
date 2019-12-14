package main

import (
	"errors"
	"net"
	"net/smtp"
)

// SenderConfig specifies the configuration to use for sending alerts.
type SenderConfig struct {
	Server      string `json:"server"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	FromName    string `json:"fromName"`
	FromAddress string `json:"fromAddress"`
}

// Site specifies a site whose heartbeat has to be monitored.
type Site struct {
	URL            string   `json:"url"`
	IsAddress      bool     `json:"isAddress"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	Recipients     []string `json:"recipients"`
}

// Config holds the monitor's configuration.
type Config struct {
	Sender           SenderConfig `json:"sender"`
	HeartbeatSeconds int          `json:"heartbeatSeconds"`
	ResolverAddress  string       `json:"resolverAddress"`
	RequestHeadOnly  bool         `json:"requestHeadOnly"`
	Sites            []Site       `json:"sites"`
}

// Monitor monitors the heartbeat of the servers specified in the
// configuration.
type Monitor struct {
	conf       *Config
	mailServer string
	senderName string
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
			return nil, errors.New("Unknown fromServer")
		}
	}
	return nil, nil
}

// LoginAuth answers an `smtp.Auth` compatible authenticator.
func LoginAuth(username, password string) smtp.Auth {
	return &loginAuth{username, password}
}
