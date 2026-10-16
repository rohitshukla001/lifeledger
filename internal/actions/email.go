package actions

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"time"
)

var ErrEmailNotConfigured = errors.New("actions: email is not configured; set LIFELEDGER_SMTP_HOST and LIFELEDGER_SMTP_FROM")

type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

type SMTP struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func (m SMTP) Send(ctx context.Context, to, subject, body string) error {
	from, err := mail.ParseAddress(m.From)
	if err != nil {
		return fmt.Errorf("actions: invalid sender %q: %w", m.From, err)
	}
	msg, err := buildMessage(from, to, subject, body, time.Now())
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(m.Host, strconv.Itoa(m.Port))
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}

	var conn net.Conn
	dialer := &net.Dialer{Deadline: deadline}
	if m.Port == 465 {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: m.Host}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("actions: connect to %s: %w", addr, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}

	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		return fmt.Errorf("actions: smtp greeting: %w", err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok && m.Port != 465 {
		if err := c.StartTLS(&tls.Config{ServerName: m.Host}); err != nil {
			return fmt.Errorf("actions: starttls: %w", err)
		}
	}
	if m.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", m.Username, m.Password, m.Host)); err != nil {
			return fmt.Errorf("actions: smtp auth: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("actions: smtp MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("actions: smtp RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("actions: smtp DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("actions: write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("actions: smtp rejected the message: %w", err)
	}
	return c.Quit()
}

func buildMessage(from *mail.Address, to, subject, body string, now time.Time) ([]byte, error) {
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from.String())
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", now.Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@lifeledger>\r\n", hex.EncodeToString(id))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")

	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(body)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
