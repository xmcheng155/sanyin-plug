package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"debug/elf"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var semverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type manifest struct {
	Format  int    `json:"format"`
	Version string `json:"version"`
	Target  string `json:"target"`
	SHA256  string `json:"sha256"`
	BuiltAt string `json:"builtAt"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "package":
		err = createPackage(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `用法：
  sanyin-update keygen -private FILE -public FILE
  sanyin-update package -private FILE -binary FILE -version X.Y.Z -output FILE`)
}

func keygen(arguments []string) error {
	flags := flag.NewFlagSet("keygen", flag.ContinueOnError)
	privateFile := flags.String("private", "", "Ed25519 私钥文件")
	publicFile := flags.String("public", "", "Ed25519 公钥文件")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *privateFile == "" || *publicFile == "" {
		return errors.New("-private 和 -public 均为必填")
	}

	var privateKey ed25519.PrivateKey
	content, err := os.ReadFile(*privateFile)
	switch {
	case err == nil:
		info, statErr := os.Stat(*privateFile)
		if statErr != nil {
			return statErr
		}
		if info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("已有私钥权限过宽：%s（应为 0600）", info.Mode().Perm())
		}
		privateKey, err = decodePrivateKey(content)
		if err != nil {
			return fmt.Errorf("已有私钥无效: %w", err)
		}
	case os.IsNotExist(err):
		_, generated, generateErr := ed25519.GenerateKey(rand.Reader)
		if generateErr != nil {
			return fmt.Errorf("生成 Ed25519 密钥失败: %w", generateErr)
		}
		privateKey = generated
		if err := os.MkdirAll(filepath.Dir(*privateFile), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(*privateFile, []byte(base64.StdEncoding.EncodeToString(privateKey)+"\n"), 0600); err != nil {
			return fmt.Errorf("写入私钥失败: %w", err)
		}
	default:
		return fmt.Errorf("读取私钥失败: %w", err)
	}

	publicKey := privateKey.Public().(ed25519.PublicKey)
	if err := os.MkdirAll(filepath.Dir(*publicFile), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(*publicFile, []byte(base64.StdEncoding.EncodeToString(publicKey)+"\n"), 0644); err != nil {
		return fmt.Errorf("写入公钥失败: %w", err)
	}
	fmt.Printf("更新签名私钥：%s（请勿上传或复制到音箱）\n", *privateFile)
	fmt.Printf("更新验证公钥：%s\n", *publicFile)
	return nil
}

func createPackage(arguments []string) error {
	flags := flag.NewFlagSet("package", flag.ContinueOnError)
	privateFile := flags.String("private", "", "Ed25519 私钥文件")
	binaryFile := flags.String("binary", "", "Linux/ARMv7 sanyin-config")
	version := flags.String("version", "", "语义版本 X.Y.Z")
	outputFile := flags.String("output", "", "输出 .sanyin-update 文件")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *privateFile == "" || *binaryFile == "" || *outputFile == "" || !semverPattern.MatchString(*version) {
		return errors.New("-private、-binary、-output 必填，-version 必须为 X.Y.Z")
	}
	if err := validateARMv7(*binaryFile); err != nil {
		return err
	}
	privateContent, err := os.ReadFile(*privateFile)
	if err != nil {
		return fmt.Errorf("读取签名私钥失败: %w", err)
	}
	privateInfo, err := os.Stat(*privateFile)
	if err != nil {
		return err
	}
	if privateInfo.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("签名私钥权限过宽：%s（应为 0600）", privateInfo.Mode().Perm())
	}
	privateKey, err := decodePrivateKey(privateContent)
	if err != nil {
		return err
	}
	binaryContent, err := os.ReadFile(*binaryFile)
	if err != nil {
		return fmt.Errorf("读取 ARMv7 程序失败: %w", err)
	}
	sum := sha256.Sum256(binaryContent)
	metadata := manifest{
		Format:  1,
		Version: *version,
		Target:  "linux/arm/v7",
		SHA256:  hex.EncodeToString(sum[:]),
		BuiltAt: time.Now().UTC().Format(time.RFC3339),
	}
	manifestContent, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	signature := ed25519.Sign(privateKey, manifestContent)

	if err := os.MkdirAll(filepath.Dir(*outputFile), 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(*outputFile), ".sanyin-update-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	archive := zip.NewWriter(temp)
	for _, item := range []struct {
		name    string
		content []byte
	}{
		{"manifest.json", manifestContent},
		{"sanyin-config", binaryContent},
		{"signature.ed25519", []byte(base64.StdEncoding.EncodeToString(signature))},
	} {
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: item.name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		if _, err := entry.Write(item.content); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, *outputFile); err != nil {
		return err
	}
	fmt.Printf("签名更新包：%s\n", *outputFile)
	fmt.Printf("版本：%s\nSHA-256：%s\n", *version, metadata.SHA256)
	return nil
}

func decodePrivateKey(content []byte) (ed25519.PrivateKey, error) {
	decoded, err := base64.StdEncoding.DecodeString(string(bytesTrimSpace(content)))
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return nil, errors.New("私钥文件不是有效的 Ed25519 私钥")
	}
	return ed25519.PrivateKey(decoded), nil
}

func bytesTrimSpace(content []byte) []byte {
	start, end := 0, len(content)
	for start < end && (content[start] == ' ' || content[start] == '\n' || content[start] == '\r' || content[start] == '\t') {
		start++
	}
	for end > start && (content[end-1] == ' ' || content[end-1] == '\n' || content[end-1] == '\r' || content[end-1] == '\t') {
		end--
	}
	return content[start:end]
}

func validateARMv7(filename string) error {
	binary, err := elf.Open(filename)
	if err != nil {
		return errors.New("输入程序不是有效 ELF")
	}
	defer binary.Close()
	if binary.Class != elf.ELFCLASS32 || binary.Data != elf.ELFDATA2LSB || binary.Machine != elf.EM_ARM {
		return fmt.Errorf("输入程序不是 Linux/ARMv7 ELF：class=%s data=%s machine=%s", binary.Class, binary.Data, binary.Machine)
	}
	for _, program := range binary.Progs {
		if program.Type == elf.PT_INTERP {
			reader := program.Open()
			content, _ := io.ReadAll(reader)
			return fmt.Errorf("输入程序依赖动态加载器 %q；请使用 CGO_ENABLED=0 构建", bytesTrimSpace(content))
		}
	}
	return nil
}
