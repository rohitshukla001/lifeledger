package actions

import (
	"bufio"
	"context"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildMessage(t *testing.T) {
	from := &mail.Address{Name: "LifeLedger", Address: "me@example.com"}
	msg, err := buildMessage(from, "billing@airtel.example", "Disputed charge – ₹100 late fee", "Hello,\nPlease waive the fee.\n", time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := mail.ReadMessage(strings.NewReader(string(msg)))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != "Disputed charge – ₹100 late fee" {
		t.Fatalf("subject = %q, %v", subject, err)
	}
	if parsed.Header.Get("To") != "billing@airtel.example" || !strings.HasSuffix(parsed.Header.Get("Message-Id"), "@lifeledger>") ||
		parsed.Header.Get("Date") != "Fri, 16 Oct 2026 10:00:00 +0000" {
		t.Fatalf("headers = %v", parsed.Header)
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(parsed.Body))
	if strings.ReplaceAll(string(body), "\r\n", "\n") != "Hello,\nPlease waive the fee.\n" {
		t.Fatalf("body = %q", body)
	}
}

type smtpCapture struct {
	from, to string
	data     string
}

func addrOf(cmd string) string {
	start, end := strings.Index(cmd, "<"), strings.Index(cmd, ">")
	if start < 0 || end < start {
		return ""
	}
	return cmd[start+1 : end]
}

func fakeSMTPServer(t *testing.T) (port int, got chan smtpCapture) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan smtpCapture, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		say := func(s string) { conn.Write([]byte(s + "\r\n")) }
		var c smtpCapture

		say("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				say("250-fake\r\n250 8BITMIME")
			case strings.HasPrefix(cmd, "MAIL FROM:"):
				c.from = addrOf(cmd)
				say("250 ok")
			case strings.HasPrefix(cmd, "RCPT TO:"):
				c.to = addrOf(cmd)
				say("250 ok")
			case cmd == "DATA":
				say("354 go")
				var b strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					b.WriteString(l)
				}
				c.data = b.String()
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				got <- c
				return
			default:
				say("502 unknown")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, got
}

func TestSMTPSend(t *testing.T) {
	port, got := fakeSMTPServer(t)
	m := SMTP{Host: "127.0.0.1", Port: port, From: "LifeLedger <me@example.com>"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Send(ctx, "billing@airtel.example", "Late fee", "Please waive it."); err != nil {
		t.Fatal(err)
	}

	c := <-got
	if c.from != "me@example.com" || c.to != "billing@airtel.example" || !strings.Contains(c.data, "Subject: Late fee") || !strings.Contains(c.data, "Please waive it.") {
		t.Fatalf("captured = %+v", c)
	}
}

func TestSMTPErrors(t *testing.T) {
	if err := (SMTP{Host: "127.0.0.1", Port: 1, From: "not an address"}).Send(context.Background(), "a@b.c", "s", "b"); err == nil || !strings.Contains(err.Error(), "invalid sender") {
		t.Fatalf("bad sender: %v", err)
	}

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	err := SMTP{Host: "127.0.0.1", Port: port, From: "me@example.com"}.Send(context.Background(), "a@b.c", "s", "b")
	if err == nil || !strings.Contains(err.Error(), "connect to 127.0.0.1:"+strconv.Itoa(port)) {
		t.Fatalf("closed port: %v", err)
	}
}
