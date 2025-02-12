package main

import (
	"context"
	"fmt"
	"net/url"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

// checkSQLServer makes a connection request to the given server, as per
// the given specification.
func (m *Monitor) checkSQLServer(site *Site) error {
	// Connection setup.
	query := url.Values{}
	query.Add("app name", "HeartBeat")

	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(site.SQLServerConfig.Username, site.SQLServerConfig.Password),
		Host:     fmt.Sprintf("%s:%d", site.Server, site.SQLServerConfig.Port),
		RawQuery: query.Encode(),
	}
	db, err := sqlx.Open("sqlserver", u.String())
	if err != nil {
		zLog.Error(site.Protocol,
			zap.String("error", err.Error()))
		return fmt.Errorf("action: connect to database, err: %s", err.Error())
	}
	defer db.Close()

	// Execute query, so that an actual connection is made.
	q := `
	SELECT TOP 1 name
	FROM sys.tables
	`
	var name string
	ctx, cFunc := context.WithDeadline(context.Background(), time.Now().Add(time.Duration(site.TimeoutMillis)*time.Millisecond))
	defer cFunc()

	tb := time.Now()
	err = db.GetContext(ctx, &name, q)
	if err != nil {
		zLog.Error(site.Protocol,
			zap.String("error", err.Error()))
		return fmt.Errorf("action: query database, err: %s", err.Error())
	}
	te := time.Now()

	zLog.Info(site.Protocol,
		zap.String("server", site.Server),
		zap.Int64("total", te.Sub(tb).Milliseconds()))
	return nil
}
