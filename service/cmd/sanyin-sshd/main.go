package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

var version = "dev"

type server struct {
	config *ssh.ServerConfig
	shell  string
}

func main() {
	listen := flag.String("listen", "0.0.0.0:22", "SSH 监听地址")
	authorizedKeys := flag.String("authorized-keys", "/mnt/UDISK/sanyin-ssh/authorized_keys", "允许登录的 OpenSSH 公钥文件")
	hostKey := flag.String("host-key", "/mnt/UDISK/sanyin-ssh/host-key", "Ed25519 SSH 主机私钥")
	shell := flag.String("shell", "/bin/sh", "认证后的命令解释器")
	showVersion := flag.Bool("version", false, "显示版本后退出")
	flag.Parse()

	if *showVersion {
		fmt.Printf("sanyin-sshd %s\n", version)
		return
	}
	service, err := newServer(*authorizedKeys, *hostKey, *shell)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	log.Printf("三音 SSH 服务 %s 已启动：%s（仅 root 公钥认证）", version, *listen)

	limit := make(chan struct{}, 8)
	for {
		connection, err := listener.Accept()
		if err != nil {
			log.Printf("接受连接失败: %v", err)
			continue
		}
		limit <- struct{}{}
		go func() {
			defer func() { <-limit }()
			service.serve(connection)
		}()
	}
}

func newServer(authorizedKeysFile, hostKeyFile, shell string) (*server, error) {
	keys, err := loadAuthorizedKeys(authorizedKeysFile)
	if err != nil {
		return nil, err
	}
	if shell == "" || !strings.HasPrefix(shell, "/") {
		return nil, errors.New("shell 必须为绝对路径")
	}
	if info, err := os.Stat(shell); err != nil || info.IsDir() || info.Mode()&0111 == 0 {
		return nil, fmt.Errorf("shell 不可执行: %s", shell)
	}
	signer, err := ensureHostKey(hostKeyFile)
	if err != nil {
		return nil, err
	}
	config := &ssh.ServerConfig{
		MaxAuthTries:  3,
		ServerVersion: "SSH-2.0-SanyinPlug_" + version,
		PublicKeyCallback: func(metadata ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if metadata.User() != "root" {
				return nil, errors.New("只允许 root 公钥登录")
			}
			if _, ok := keys[string(key.Marshal())]; !ok {
				return nil, errors.New("公钥未授权")
			}
			return &ssh.Permissions{Extensions: map[string]string{"pubkey-fingerprint": ssh.FingerprintSHA256(key)}}, nil
		},
	}
	config.AddHostKey(signer)
	return &server{config: config, shell: shell}, nil
}

func loadAuthorizedKeys(filename string) (map[string]struct{}, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("读取 authorized_keys 失败: %w", err)
	}
	defer file.Close()
	keys := map[string]struct{}{}
	scanner := bufio.NewScanner(io.LimitReader(file, 1<<20))
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		key, _, _, _, err := ssh.ParseAuthorizedKey(line)
		if err != nil {
			return nil, fmt.Errorf("authorized_keys 包含无效公钥: %w", err)
		}
		keys[string(key.Marshal())] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, errors.New("authorized_keys 没有有效公钥")
	}
	return keys, nil
}

func ensureHostKey(filename string) (ssh.Signer, error) {
	content, err := os.ReadFile(filename)
	if err == nil {
		signer, parseErr := ssh.ParsePrivateKey(content)
		if parseErr != nil {
			return nil, fmt.Errorf("SSH 主机私钥无效: %w", parseErr)
		}
		return signer, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return nil, err
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	temporary := filename + ".tmp"
	if err := os.WriteFile(temporary, block, 0600); err != nil {
		return nil, err
	}
	if err := os.Chmod(temporary, 0600); err != nil {
		_ = os.Remove(temporary)
		return nil, err
	}
	if err := os.Rename(temporary, filename); err != nil {
		_ = os.Remove(temporary)
		return nil, err
	}
	return ssh.NewSignerFromKey(privateKey)
}

func (s *server) serve(connection net.Conn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(15 * time.Second))
	serverConnection, channels, requests, err := ssh.NewServerConn(connection, s.config)
	if err != nil {
		return
	}
	_ = connection.SetDeadline(time.Time{})
	defer serverConnection.Close()
	go ssh.DiscardRequests(requests)

	for channelRequest := range channels {
		if channelRequest.ChannelType() != "session" {
			_ = channelRequest.Reject(ssh.UnknownChannelType, "只允许 session")
			continue
		}
		channel, channelRequests, err := channelRequest.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(channel, channelRequests)
	}
}

func (s *server) handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for request := range requests {
		switch request.Type {
		case "exec":
			var payload struct{ Command string }
			if err := ssh.Unmarshal(request.Payload, &payload); err != nil || payload.Command == "" || len(payload.Command) > 65535 || strings.ContainsRune(payload.Command, '\x00') {
				_ = request.Reply(false, nil)
				return
			}
			_ = request.Reply(true, nil)
			s.run(channel, payload.Command, false)
			return
		case "shell":
			if len(request.Payload) != 0 {
				_ = request.Reply(false, nil)
				return
			}
			_ = request.Reply(true, nil)
			s.run(channel, "", true)
			return
		default:
			_ = request.Reply(false, nil)
		}
	}
}

func (s *server) run(channel ssh.Channel, commandText string, interactive bool) {
	var command *exec.Cmd
	if interactive {
		command = exec.Command(s.shell)
	} else {
		command = exec.Command(s.shell, "-c", commandText)
	}
	command.Dir = "/"
	command.Env = []string{
		"HOME=/root",
		"USER=root",
		"LOGNAME=root",
		"SHELL=" + s.shell,
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
		"TERM=dumb",
	}
	command.Stdin = channel
	command.Stdout = channel
	command.Stderr = channel.Stderr()
	err := command.Run()
	status := uint32(0)
	if err != nil {
		status = 1
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() >= 0 {
			status = uint32(exit.ExitCode())
		}
	}
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
}
