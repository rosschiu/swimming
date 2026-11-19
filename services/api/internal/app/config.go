package app

import (
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL, Addr, Origin                           string
	SMTPAddr, SMTPFrom, SMTPTLS, SMTPUser, SMTPPassword string
	TeachingEmailAllowlist                              []string
	CookieSecure                                        bool
	SessionTTL                                          time.Duration
	// MediaDir holds note photos, recordings and videos. Uploads are refused when it is empty.
	MediaDir string
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
	c.MediaDir = os.Getenv("MEDIA_DIR")
	if state, _, _ := strings.Cut(os.Getenv("STATE_DIRECTORY"), ":"); c.MediaDir == "" && state != "" {
		c.MediaDir = filepath.Join(state, "media")
	}
	c.SMTPAddr = os.Getenv("SMTP_ADDR")
	c.SMTPFrom = os.Getenv("SMTP_FROM")
	c.SMTPTLS = os.Getenv("SMTP_TLS")
	c.SMTPUser = os.Getenv("SMTP_USER")
	c.SMTPPassword = os.Getenv("SMTP_PASSWORD")
	if c.SMTPTLS == "" {
		c.SMTPTLS = "starttls"
	}
	if c.SMTPTLS != "none" && c.SMTPTLS != "starttls" && c.SMTPTLS != "tls" {
		return c, fmt.Errorf("SMTP_TLS must be none, starttls or tls")
	}
	if c.SMTPTLS == "none" && (c.SMTPUser != "" || c.SMTPPassword != "") {
		return c, fmt.Errorf("SMTP authentication requires TLS")
	}

	for _, value := range strings.Split(os.Getenv("TEACHING_EMAIL_ALLOWLIST"), ",") {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		addr, err := mail.ParseAddress(value)
		if err != nil || addr.Address != value || strings.ContainsAny(value, "\r\n") {
			return c, fmt.Errorf("TEACHING_EMAIL_ALLOWLIST requires exact email addresses")
		}
		c.TeachingEmailAllowlist = append(c.TeachingEmailAllowlist, value)
	}
	return c, nil
}
