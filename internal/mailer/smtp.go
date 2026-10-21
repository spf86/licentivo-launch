package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	To, Subject, Body string
	IdempotencyKey    string
}
type Sender interface {
	Send(context.Context, Message) error
}
type Config struct {
	Host, Port, Username, Password, From string
	ImplicitTLS                          bool
}
type SMTP struct {
	config    Config
	dial      func(context.Context, string, *tls.Config, bool) (net.Conn, error)
	tlsConfig *tls.Config
}

var ErrDelivery = errors.New("mail delivery failed")

func NewSMTP(c Config) (*SMTP, error) {
	from, err := mail.ParseAddress(c.From)
	port, portErr := strconv.Atoi(c.Port)
	if err != nil || from.Address != c.From || c.Host == "" || strings.ContainsAny(c.Host+c.Port+c.Username+c.From, "\r\n") || portErr != nil || port < 1 || port > 65535 {
		return nil, errors.New("invalid SMTP configuration")
	}
	return &SMTP{config: c}, nil
}

func (s *SMTP) Send(ctx context.Context, message Message) error {
	to, err := mail.ParseAddress(message.To)
	if err != nil || to.Address != message.To || strings.ContainsAny(message.To+message.Subject+message.IdempotencyKey, "\r\n") {
		return ErrDelivery
	}
	address := net.JoinHostPort(s.config.Host, s.config.Port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	tlsConfig := &tls.Config{ServerName: s.config.Host, MinVersion: tls.VersionTLS12}
	if s.tlsConfig != nil {
		tlsConfig = s.tlsConfig.Clone()
	}
	if s.dial != nil {
		conn, err = s.dial(ctx, address, tlsConfig, s.config.ImplicitTLS)
	} else if s.config.ImplicitTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", address)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return ErrDelivery
	}
	defer conn.Close()
	deadline := time.Now().Add(15 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	client, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		return ErrDelivery
	}
	defer client.Close()
	if !s.config.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return ErrDelivery
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return ErrDelivery
		}
	}
	if s.config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)); err != nil {
			return ErrDelivery
		}
	}
	if client.Mail(s.config.From) != nil || client.Rcpt(message.To) != nil {
		return ErrDelivery
	}
	writer, err := client.Data()
	if err != nil {
		return ErrDelivery
	}
	body := strings.ReplaceAll(strings.ReplaceAll(message.Body, "\r\n", "\n"), "\n", "\r\n")
	idempotency := ""
	if message.IdempotencyKey != "" {
		idempotency = "Resend-Idempotency-Key: " + message.IdempotencyKey + "\r\n"
	}
	_, err = fmt.Fprintf(writer, "From: %s\r\nTo: %s\r\nSubject: %s\r\n%sMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", s.config.From, message.To, mime.QEncoding.Encode("UTF-8", message.Subject), idempotency, body)
	if err != nil {
		_ = writer.Close()
		return ErrDelivery
	}
	if writer.Close() != nil {
		return ErrDelivery
	}
	// DATA acceptance is the delivery handoff; a failed QUIT must not resend it.
	_ = client.Quit()
	return nil
}
