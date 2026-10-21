package mailer

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func testCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	parsed, _ := x509.ParseCertificate(raw)
	roots.AddCert(parsed)
	return cert, roots
}
func smtpPipe(t *testing.T, cert tls.Certificate, implicit, advertiseTLS bool) (func(context.Context, string, *tls.Config, bool) (net.Conn, error), <-chan string) {
	t.Helper()
	messages := make(chan string, 1)
	dial := func(ctx context.Context, _ string, tlsConfig *tls.Config, _ bool) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			defer close(messages)
			_ = server.SetDeadline(time.Now().Add(3 * time.Second))
			var connection net.Conn = server
			if implicit {
				secured := tls.Server(server, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
				if secured.Handshake() != nil {
					return
				}
				connection = secured
			}
			reader := textproto.NewReader(bufio.NewReader(connection))
			write := func(v string) bool { _, err := connection.Write([]byte(v + "\r\n")); return err == nil }
			if !write("220 localhost SMTP") {
				return
			}
			for {
				line, err := reader.ReadLine()
				if err != nil {
					return
				}
				switch {
				case strings.HasPrefix(line, "EHLO") || strings.HasPrefix(line, "HELO"):
					reply := "250-localhost\r\n250 AUTH PLAIN"
					if advertiseTLS {
						reply = "250-localhost\r\n250-STARTTLS\r\n250 AUTH PLAIN"
					}
					if !write(reply) {
						return
					}
				case line == "STARTTLS":
					if !write("220 Ready") {
						return
					}
					secured := tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
					if secured.Handshake() != nil {
						return
					}
					connection = secured
					reader = textproto.NewReader(bufio.NewReader(connection))
				case strings.HasPrefix(line, "AUTH PLAIN"):
					if _, ok := connection.(*tls.Conn); !ok {
						t.Error("SMTP transmitted authentication without TLS")
					}
					if !write("235 Authenticated") {
						return
					}
				case strings.HasPrefix(line, "MAIL FROM:") || strings.HasPrefix(line, "RCPT TO:"):
					if !write("250 OK") {
						return
					}
				case line == "DATA":
					if !write("354 Send data") {
						return
					}
					lines, err := reader.ReadDotLines()
					if err != nil {
						return
					}
					messages <- strings.Join(lines, "\n")
					if !write("250 Accepted") {
						return
					}
				case line == "QUIT":
					return
				default:
					t.Errorf("unexpected SMTP command: %s", line)
					return
				}
			}
		}()
		if implicit {
			secured := tls.Client(client, tlsConfig)
			if err := secured.HandshakeContext(ctx); err != nil {
				client.Close()
				return nil, err
			}
			return secured, nil
		}
		return client, nil
	}
	return dial, messages
}
func TestSMTPVerifiedTLSAndAcceptedData(t *testing.T) {
	cert, roots := testCertificate(t)
	for _, implicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "STARTTLS", true: "implicit_TLS"}[implicit], func(t *testing.T) {
			sender, err := NewSMTP(Config{Host: "localhost", Port: "587", From: "sender@example.invalid", Username: "test-user", Password: "test-password", ImplicitTLS: implicit})
			if err != nil {
				t.Fatal(err)
			}
			sender.tlsConfig = &tls.Config{ServerName: "localhost", MinVersion: tls.VersionTLS12, RootCAs: roots}
			dial, messages := smtpPipe(t, cert, implicit, true)
			sender.dial = dial
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := sender.Send(ctx, Message{To: "customer@example.invalid", Subject: "邮箱验证", Body: "Line one\n.leading dot\nLine three", IdempotencyKey: "launch-mail/test-message"}); err != nil {
				t.Fatal(err)
			}
			message := <-messages
			if !strings.Contains(message, ".leading dot") || !strings.Contains(message, "Subject: =?UTF-8?") || !strings.Contains(message, "Resend-Idempotency-Key: launch-mail/test-message") {
				t.Fatal("MIME encoding or dot transparency failed")
			}
		})
	}
}
func TestSMTPRefusesDowngradeBadCertificateAndHeaderInjection(t *testing.T) {
	cert, _ := testCertificate(t)
	for _, test := range []struct {
		name      string
		advertise bool
		message   Message
	}{{"missing_STARTTLS", false, Message{To: "customer@example.invalid", Subject: "test"}}, {"untrusted_certificate", true, Message{To: "customer@example.invalid", Subject: "test"}}, {"header_injection", true, Message{To: "customer@example.invalid", Subject: "test\r\nBcc: attacker@example.invalid"}}, {"idempotency_header_injection", true, Message{To: "customer@example.invalid", Subject: "test", IdempotencyKey: "safe\r\nBcc: attacker@example.invalid"}}} {
		t.Run(test.name, func(t *testing.T) {
			sender, _ := NewSMTP(Config{Host: "localhost", Port: "587", From: "sender@example.invalid", Username: "test", Password: "test"})
			dial, _ := smtpPipe(t, cert, false, test.advertise)
			sender.dial = dial
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := sender.Send(ctx, test.message); err == nil {
				t.Fatal("unsafe SMTP delivery accepted")
			}
		})
	}
}
