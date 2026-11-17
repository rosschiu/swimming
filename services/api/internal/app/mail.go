package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"mime/quotedprintable"
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
	path, subject := "/accept-invitation?token=", "SwimCoach Pro invitation"
	if purpose == "password_reset" {
		path = "/reset-password?token="
		subject = "SwimCoach Pro password reset"
	}
	body := subject + "\r\n\r\nOpen this link to set your password:\r\n" + a.cfg.Origin + path + token + "\r\n\r\nIf you did not expect this email, ignore it. This link is temporary and can only be used once.\r\n"
	return a.smtpSendMessage(ctx, recipient, subject, body, "")
}
func (a *API) sendTeachingMail(ctx context.Context, claim notificationClaim) error {
	body := "You have a shared teaching update. Sign in to view it.\r\n您有新的教學近況。請登入查看。\r\n\r\n" + a.cfg.Origin + "/notifications\r\n"
	return a.smtpSendMessage(ctx, claim.Recipient, "SwimCoach Pro update / 教學近況", body, "<teaching-"+claim.NotificationID+"@swimcoach.invalid>")
}
func (a *API) smtpSendMessage(ctx context.Context, recipient, subject, body, messageID string) error {
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
	rawConn := conn
	stopClose := context.AfterFunc(ctx, func() { rawConn.Close() })
	defer stopClose()
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
	headers := ""
	if messageID != "" {
		headers = "Message-ID: " + messageID + "\r\n"
	}

	// Teaching messages contain Chinese; encode them for SMTP transports without 8BITMIME.
	if messageID != "" {
		var encoded bytes.Buffer
		qp := quotedprintable.NewWriter(&encoded)
		if _, err = qp.Write([]byte(body)); err != nil {
			return err
		}
		if err = qp.Close(); err != nil {
			return err
		}
		body = encoded.String()
		headers += "Content-Transfer-Encoding: quoted-printable\r\n"
	}
	message := "From: " + from.String() + "\r\nTo: " + to.Address + "\r\nSubject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n" + headers + "MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	if _, err = writer.Write([]byte(message)); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	client.Quit() // DATA acceptance already confirms SMTP handoff.
	return nil
}
