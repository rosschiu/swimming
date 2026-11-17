package app

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL, Addr, Origin string
	CookieSecure              bool
	SessionTTL                time.Duration
}

func LoadConfig() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Addr: os.Getenv("ADDR"), Origin: os.Getenv("APP_ORIGIN"), CookieSecure: true, SessionTTL: 12 * time.Hour}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if c.Addr == "" {
		c.Addr = "127.0.0.1:18083"
	}
	if c.Origin == "" {
		c.Origin = "http://localhost:18573"
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return c, fmt.Errorf("APP_ORIGIN must be an HTTP(S) origin without a path")
	}
	if value := os.Getenv("COOKIE_SECURE"); value != "" {
		c.CookieSecure, err = strconv.ParseBool(value)
		if err != nil {
			return c, fmt.Errorf("COOKIE_SECURE must be a boolean")
		}
	}
	return c, nil
}
