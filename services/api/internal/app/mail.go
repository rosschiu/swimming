package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// sendAuthMail keeps the credential link only in memory while sending. A failed
// or interrupted delivery is recovered by issuing a new token through resend.
func (a *API) sendAuthMail(ctx context.Context, deliveryID int64, recipient, purpose, token string) string {
	status := "sent"
	if err := a.smtpSend(ctx, recipient, purpose, token); err != nil {
		status = "failed"
	}
	// Record delivery even if the browser disconnected after the mutation committed.
	save, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.db.Exec(save, "UPDATE email_deliveries SET status=$2,attempts=attempts+1,completed_at=now() WHERE id=$1", deliveryID, status); err != nil {
		return "pending"
	}
	return status
}
func (a *API) smtpSend(ctx context.Context, recipient, purpose, token string) error {
	if a.cfg.SMTPAddr == "" || a.cfg.SMTPFrom == "" {
		return fmt.Errorf("SMTP is not configured")
	}
	from, err := mail.ParseAddress(a.cfg.SMTPFrom)
	if err != nil || strings.ContainsAny(a.cfg.SMTPFrom, "\r\n") {
		return fmt.Errorf("invalid sender")
	}
	to, err := mail.ParseAddress(recipient)
	if err != nil || to.Address != recipient || strings.ContainsAny(recipient, "\r\n") {
		return fmt.Errorf("invalid recipient")
	}
	host, _, err := net.SplitHostPort(a.cfg.SMTPAddr)
	if err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", a.cfg.SMTPAddr)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(8 * time.Second))
	security := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if a.cfg.SMTPTLS == "tls" {
		secure := tls.Client(conn, security)
		if err = secure.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if a.cfg.SMTPTLS != "none" && a.cfg.SMTPTLS != "tls" {
		if err = client.StartTLS(security); err != nil {
			return err
		}
	}
	if a.cfg.SMTPUser != "" {
		if a.cfg.SMTPTLS == "none" {
			return fmt.Errorf("authentication requires TLS")
		}
		if err = client.Auth(smtp.PlainAuth("", a.cfg.SMTPUser, a.cfg.SMTPPassword, host)); err != nil {
			return err
		}
	}
	if err = client.Mail(from.Address); err != nil {
		return err
	}
	if err = client.Rcpt(to.Address); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	path, subject := "/accept-invitation?token=", "SwimCoach Pro invitation"
	if purpose == "password_reset" {
		path = "/reset-password?token="
		subject = "SwimCoach Pro password reset"
	}
	message := "From: " + from.String() + "\r\nTo: " + to.Address + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + subject + "\r\n\r\nOpen this link to set your password:\r\n" + a.cfg.Origin + path + token + "\r\n\r\nIf you did not expect this email, ignore it. This link is temporary and can only be used once.\r\n"
	if _, err = writer.Write([]byte(message)); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	client.Quit() // DATA acceptance already confirms SMTP handoff.
	return nil
}
