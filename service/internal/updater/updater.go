package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"debug/elf"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"sanyin.local/config/service/internal/domain"
)

const (
	defaultMaxPackageBytes = 32 << 20
	manifestName           = "manifest.json"
	binaryName             = "sanyin-config"
	signatureName          = "signature.ed25519"
	candidateName          = "sanyin-config.candidate"
	statusName             = "update-status"
)

var (
	ErrDisabled         = errors.New("网页更新未启用")
	ErrBusy             = errors.New("已有更新正在处理")
	ErrInvalidPackage   = errors.New("更新包格式无效")
	ErrInvalidSignature = errors.New("更新包签名无效")
	ErrNotNewer         = errors.New("更新版本不高于当前版本")
	semverPattern       = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

type Config struct {
	Build          domain.BuildInfo
	PublicKeyFile  string
	StateDir       string
	ApplyScript    string
	Target         string
	TargetClass    elf.Class
	TargetMachine  elf.Machine
	MaxPackageSize int64
	StartApply     func(string) error
	Now            func() time.Time
}

type Manager struct {
	config Config
	mu     sync.Mutex
}

type manifest struct {
	Format  int    `json:"format"`
	Version string `json:"version"`
	Target  string `json:"target"`
	SHA256  string `json:"sha256"`
	BuiltAt string `json:"builtAt"`
}

func NewManager(config Config) *Manager {
	if config.MaxPackageSize <= 0 {
		config.MaxPackageSize = defaultMaxPackageBytes
	}
	if config.Target == "" {
		config.Target = "linux/arm/v7"
	}
	if config.TargetClass == elf.ELFCLASSNONE {
		config.TargetClass = elf.ELFCLASS32
	}
	if config.TargetMachine == elf.EM_NONE {
		config.TargetMachine = elf.EM_ARM
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.StartApply == nil {
		config.StartApply = startApply
	}
	return &Manager{config: config}
}

func (m *Manager) Info() domain.SystemInfo {
	enabled := false
	if m.config.PublicKeyFile != "" && m.config.StateDir != "" && m.config.ApplyScript != "" {
		script, scriptErr := os.Stat(m.config.ApplyScript)
		if _, err := readPublicKey(m.config.PublicKeyFile); err == nil && scriptErr == nil && !script.IsDir() && script.Mode()&0111 != 0 {
			enabled = true
		}
	}
	status := domain.UpdateStatus{State: "idle"}
	if m.config.StateDir != "" {
		if parsed, err := readStatus(filepath.Join(m.config.StateDir, statusName)); err == nil {
			status = parsed
		}
	}
	return domain.SystemInfo{Build: m.config.Build, UpdateEnabled: enabled, Update: status}
}

func (m *Manager) Stage(ctx context.Context, reader io.Reader, contentLength int64) (domain.UpdateAccepted, error) {
	if !m.Info().UpdateEnabled {
		return domain.UpdateAccepted{}, ErrDisabled
	}
	if !m.mu.TryLock() {
		return domain.UpdateAccepted{}, ErrBusy
	}
	defer m.mu.Unlock()

	if contentLength > m.config.MaxPackageSize {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: 文件超过 %d MiB", ErrInvalidPackage, m.config.MaxPackageSize>>20)
	}
	if err := os.MkdirAll(m.config.StateDir, 0700); err != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("创建更新目录失败: %w", err)
	}

	upload, err := os.CreateTemp(m.config.StateDir, ".update-upload-*")
	if err != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("创建上传临时文件失败: %w", err)
	}
	uploadName := upload.Name()
	defer os.Remove(uploadName)

	written, copyErr := io.Copy(upload, io.LimitReader(reader, m.config.MaxPackageSize+1))
	closeErr := upload.Close()
	if copyErr != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("接收更新包失败: %w", copyErr)
	}
	if closeErr != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("保存更新包失败: %w", closeErr)
	}
	if written == 0 || written > m.config.MaxPackageSize {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: 文件为空或超过大小限制", ErrInvalidPackage)
	}

	archive, err := zip.OpenReader(uploadName)
	if err != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: 无法读取 ZIP 容器", ErrInvalidPackage)
	}
	defer archive.Close()

	entries, err := indexedEntries(archive.File)
	if err != nil {
		return domain.UpdateAccepted{}, err
	}
	manifestBytes, err := readSmallEntry(entries[manifestName], 16<<10)
	if err != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: manifest.json 不可读", ErrInvalidPackage)
	}
	signatureBytes, err := readSmallEntry(entries[signatureName], 1024)
	if err != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: signature.ed25519 不可读", ErrInvalidPackage)
	}

	var metadata manifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: manifest.json 字段无效", ErrInvalidPackage)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: manifest.json 只能包含一个对象", ErrInvalidPackage)
	}
	if metadata.Format != 1 || !semverPattern.MatchString(metadata.Version) || metadata.Target != m.config.Target {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: 版本、格式或目标平台不匹配", ErrInvalidPackage)
	}
	if _, err := hex.DecodeString(metadata.SHA256); err != nil || len(metadata.SHA256) != sha256.Size*2 {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: SHA-256 字段无效", ErrInvalidPackage)
	}
	if current := m.config.Build.Version; semverPattern.MatchString(current) && compareSemver(metadata.Version, current) <= 0 {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: 当前 %s，更新包 %s", ErrNotNewer, current, metadata.Version)
	}

	publicKey, err := readPublicKey(m.config.PublicKeyFile)
	if err != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("读取更新公钥失败: %w", err)
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signatureBytes)))
	if err != nil || !ed25519.Verify(publicKey, manifestBytes, signature) {
		return domain.UpdateAccepted{}, ErrInvalidSignature
	}

	candidateTemp, err := os.CreateTemp(m.config.StateDir, ".candidate-*")
	if err != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("创建候选程序失败: %w", err)
	}
	candidateTempName := candidateTemp.Name()
	defer os.Remove(candidateTempName)

	binaryReader, err := entries[binaryName].Open()
	if err != nil {
		candidateTemp.Close()
		return domain.UpdateAccepted{}, fmt.Errorf("%w: 程序文件不可读", ErrInvalidPackage)
	}
	hash := sha256.New()
	binarySize, copyErr := io.Copy(io.MultiWriter(candidateTemp, hash), io.LimitReader(binaryReader, m.config.MaxPackageSize+1))
	readerCloseErr := binaryReader.Close()
	fileCloseErr := candidateTemp.Close()
	if copyErr != nil || readerCloseErr != nil || fileCloseErr != nil || binarySize == 0 || binarySize > m.config.MaxPackageSize {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: 程序文件为空、过大或不完整", ErrInvalidPackage)
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), metadata.SHA256) {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: 程序 SHA-256 与清单不一致", ErrInvalidPackage)
	}
	if err := validateELF(candidateTempName, m.config.TargetClass, m.config.TargetMachine); err != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	if err := os.Chmod(candidateTempName, 0755); err != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("设置候选程序权限失败: %w", err)
	}

	candidatePath := filepath.Join(m.config.StateDir, candidateName)
	if err := os.Rename(candidateTempName, candidatePath); err != nil {
		return domain.UpdateAccepted{}, fmt.Errorf("发布候选程序失败: %w", err)
	}
	staged := domain.UpdateStatus{
		State:     "staged",
		Version:   metadata.Version,
		Message:   "签名和程序校验通过，等待服务重启",
		UpdatedAt: m.config.Now().UTC().Format(time.RFC3339),
	}
	if err := writeStatus(filepath.Join(m.config.StateDir, statusName), staged); err != nil {
		os.Remove(candidatePath)
		return domain.UpdateAccepted{}, fmt.Errorf("写入更新状态失败: %w", err)
	}
	if err := m.config.StartApply(m.config.ApplyScript); err != nil {
		os.Remove(candidatePath)
		failed := staged
		failed.State = "failed"
		failed.Message = "无法启动设备侧更新器"
		failed.UpdatedAt = m.config.Now().UTC().Format(time.RFC3339)
		_ = writeStatus(filepath.Join(m.config.StateDir, statusName), failed)
		return domain.UpdateAccepted{}, fmt.Errorf("启动设备侧更新器失败: %w", err)
	}
	return domain.UpdateAccepted{
		Version: metadata.Version,
		State:   "staged",
		Message: "更新包已验证；服务将重启并执行健康检查，失败时自动回滚",
	}, nil
}

func indexedEntries(files []*zip.File) (map[string]*zip.File, error) {
	if len(files) != 3 {
		return nil, fmt.Errorf("%w: 更新包必须只包含 3 个文件", ErrInvalidPackage)
	}
	entries := make(map[string]*zip.File, len(files))
	for _, file := range files {
		if file.FileInfo().IsDir() || (file.Name != manifestName && file.Name != binaryName && file.Name != signatureName) {
			return nil, fmt.Errorf("%w: 包含未授权路径或文件", ErrInvalidPackage)
		}
		if entries[file.Name] != nil {
			return nil, fmt.Errorf("%w: 包含重复文件", ErrInvalidPackage)
		}
		entries[file.Name] = file
	}
	return entries, nil
}

func readSmallEntry(entry *zip.File, limit int64) ([]byte, error) {
	if entry == nil || int64(entry.UncompressedSize64) > limit {
		return nil, ErrInvalidPackage
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, limit+1))
}

func readPublicKey(filename string) (ed25519.PublicKey, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(content)))
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return nil, errors.New("公钥文件不是有效的 Ed25519 公钥")
	}
	return ed25519.PublicKey(decoded), nil
}

func validateELF(filename string, class elf.Class, machine elf.Machine) error {
	binary, err := elf.Open(filename)
	if err != nil {
		return errors.New("候选程序不是有效 ELF")
	}
	defer binary.Close()
	if binary.Class != class || binary.Machine != machine || binary.Data != elf.ELFDATA2LSB {
		return fmt.Errorf("ELF 平台不匹配：class=%s machine=%s data=%s", binary.Class, binary.Machine, binary.Data)
	}
	if binary.Type != elf.ET_EXEC && binary.Type != elf.ET_DYN {
		return fmt.Errorf("ELF 类型不允许：%s", binary.Type)
	}
	return nil
}

func compareSemver(left, right string) int {
	parse := func(value string) [3]int {
		match := semverPattern.FindStringSubmatch(value)
		var result [3]int
		for index := 0; index < 3; index++ {
			result[index], _ = strconv.Atoi(match[index+1])
		}
		return result
	}
	a, b := parse(left), parse(right)
	for index := 0; index < 3; index++ {
		if a[index] < b[index] {
			return -1
		}
		if a[index] > b[index] {
			return 1
		}
	}
	return 0
}

func writeStatus(filename string, status domain.UpdateStatus) error {
	temp := filename + ".tmp"
	content := fmt.Sprintf("state=%s\nversion=%s\nmessage=%s\nupdated_at=%s\n",
		status.State, status.Version, strings.ReplaceAll(status.Message, "\n", " "), status.UpdatedAt)
	if err := os.WriteFile(temp, []byte(content), 0600); err != nil {
		return err
	}
	return os.Rename(temp, filename)
}

func readStatus(filename string) (domain.UpdateStatus, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return domain.UpdateStatus{}, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(content), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	state := values["state"]
	if state == "" {
		return domain.UpdateStatus{}, errors.New("状态文件缺少 state")
	}
	return domain.UpdateStatus{
		State:     state,
		Version:   values["version"],
		Message:   values["message"],
		UpdatedAt: values["updated_at"],
	}, nil
}

func startApply(script string) error {
	command := exec.Command("/bin/sh", "-c", `"$1" >/tmp/sanyin_update.log 2>&1 </dev/null &`, "sanyin-update", script)
	return command.Run()
}
