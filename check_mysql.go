package main

import (
	"context"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

// checkMySQL makes a connection request to the given server, as per the
// given specification.
func (m *Monitor) checkMySQL(site *Site) error {
	// Connection setup.
	dbConf := mysql.NewConfig()
	dbConf.User = site.MySQLConfig.Username
	dbConf.Passwd = site.MySQLConfig.Password
	dbConf.Net = "tcp"
	dbConf.Addr = fmt.Sprintf("%s:%d", site.Server, site.MySQLConfig.Port)
	dbConf.InterpolateParams = true
	dbConf.ParseTime = true
	db, err := sqlx.Open("mysql", dbConf.FormatDSN())
	if err != nil {
		zLog.Error(site.Protocol,
			zap.String("error", err.Error()))
		return fmt.Errorf("action: connect to database, err: %s", err.Error())
	}
	defer db.Close()

	// Execute query, so that an actual connection is made.
	q := `
	SELECT table_name
	FROM information_schema.tables
	LIMIT 1
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
