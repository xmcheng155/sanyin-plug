package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestPublicKeyOnlySSHExecutesCommandsAndRejectsOtherUsers(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPublic, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	authorized := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(authorized, ssh.MarshalAuthorizedKey(sshPublic), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := newServer(authorized, filepath.Join(dir, "host-key"), "/bin/sh")
	if err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			service.serve(connection)
		}
	}()
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 测试使用一次性监听器和主机密钥。
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	output, err := session.Output("printf sanyin-ssh-ok")
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "sanyin-ssh-ok" {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestAuthorizedKeysAndHostKeyValidation(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadAuthorizedKeys(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing authorized_keys was accepted")
	}
	invalid := filepath.Join(dir, "invalid")
	if err := os.WriteFile(invalid, []byte("not-a-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAuthorizedKeys(invalid); err == nil {
		t.Fatal("invalid authorized key was accepted")
	}
	hostKey := filepath.Join(dir, "host-key")
	first, err := ensureHostKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ensureHostKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(ssh.FingerprintSHA256(first.PublicKey()), ssh.FingerprintSHA256(second.PublicKey())) {
		t.Fatal("host key was not persistent")
	}
	content, err := os.ReadFile(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(content)
	if block == nil {
		t.Fatal("host key is not PEM")
	}
	if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
		t.Fatal(err)
	}
}
