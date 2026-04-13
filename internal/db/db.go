package db

import (
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

func Init(dsn string) error {
	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(10)
	return DB.Ping()
}

func Close() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}
