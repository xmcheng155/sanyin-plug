package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"debug/elf"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sanyin.local/config/service/internal/domain"
)

func TestSignedPackageIsStagedAndApplyStarts(t *testing.T) {
	manager, privateKey, stateDir, started := testManager(t)
	pkg := signedPackage(t, privateKey, "1.1.0", "linux/test", executableBytes())

	accepted, err := manager.Stage(context.Background(), bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Version != "1.1.0" || accepted.State != "staged" {
		t.Fatalf("unexpected accepted response: %#v", accepted)
	}
	if _, err := os.Stat(filepath.Join(stateDir, candidateName)); err != nil {
		t.Fatalf("candidate was not staged: %v", err)
	}
	if len(*started) != 1 || (*started)[0] != filepath.Join(stateDir, "sanyin_apply_update.sh") {
		t.Fatalf("apply script was not started: %#v", *started)
	}
	info := manager.Info()
	if info.Update.State != "staged" || info.Update.Version != "1.1.0" {
		t.Fatalf("staged status was not persisted: %#v", info)
	}
}

func TestTamperedManifestAndBinaryAreRejected(t *testing.T) {
	manager, privateKey, stateDir, started := testManager(t)
	binary := executableBytes()

	t.Run("invalid signature", func(t *testing.T) {
		otherPublic, otherPrivate, err := ed25519.GenerateKey(rand.Reader)
		if err != nil || len(otherPublic) == 0 {
			t.Fatal(err)
		}
		pkg := signedPackage(t, otherPrivate, "1.1.0", "linux/test", binary)
		_, err = manager.Stage(context.Background(), bytes.NewReader(pkg), int64(len(pkg)))
		if !errors.Is(err, ErrInvalidSignature) {
			t.Fatalf("expected signature error, got %v", err)
		}
	})

	t.Run("binary hash mismatch", func(t *testing.T) {
		pkg := signedPackageWithHash(t, privateKey, "1.1.0", "linux/test", binary, stringsOfByte("0", 64))
		_, err := manager.Stage(context.Background(), bytes.NewReader(pkg), int64(len(pkg)))
		if !errors.Is(err, ErrInvalidPackage) {
			t.Fatalf("expected package error, got %v", err)
		}
	})

	if len(*started) != 0 {
		t.Fatalf("invalid package started apply script: %#v", *started)
	}
	if _, err := os.Stat(filepath.Join(stateDir, candidateName)); !os.IsNotExist(err) {
		t.Fatalf("invalid package left a candidate behind: %v", err)
	}
}

func TestDowngradeAndWrongTargetAreRejected(t *testing.T) {
	manager, privateKey, _, _ := testManager(t)
	binary := executableBytes()
	for name, item := range map[string]struct {
		version  string
		target   string
		expected error
	}{
		"same":         {"1.0.0", "linux/test", ErrNotNewer},
		"older":        {"0.9.9", "linux/test", ErrNotNewer},
		"wrong target": {"1.1.0", "linux/mips", ErrInvalidPackage},
	} {
		t.Run(name, func(t *testing.T) {
			pkg := signedPackage(t, privateKey, item.version, item.target, binary)
			_, err := manager.Stage(context.Background(), bytes.NewReader(pkg), int64(len(pkg)))
			if !errors.Is(err, item.expected) {
				t.Fatalf("expected %v, got %v", item.expected, err)
			}
		})
	}
}

func TestUpdateStaysDisabledWithoutValidKeyAndApplyScript(t *testing.T) {
	stateDir := t.TempDir()
	keyFile := filepath.Join(stateDir, "update-public-key")
	if err := os.WriteFile(keyFile, []byte("not-a-public-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{
		PublicKeyFile: keyFile,
		StateDir:      stateDir,
		ApplyScript:   filepath.Join(stateDir, "missing-script"),
	})
	if manager.Info().UpdateEnabled {
		t.Fatal("invalid key and missing apply script enabled system updates")
	}
	if _, err := manager.Stage(context.Background(), bytes.NewReader([]byte("package")), 7); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled updater returned %v", err)
	}
}

func testManager(t *testing.T) (*Manager, ed25519.PrivateKey, string, *[]string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	publicKeyFile := filepath.Join(stateDir, "update-public-key")
	if err := os.WriteFile(publicKeyFile, []byte(base64.StdEncoding.EncodeToString(publicKey)), 0600); err != nil {
		t.Fatal(err)
	}
	applyScript := filepath.Join(stateDir, "sanyin_apply_update.sh")
	if err := os.WriteFile(applyScript, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	started := []string{}
	manager := NewManager(Config{
		Build:         domain.BuildInfo{Version: "1.0.0", Commit: "test", BuiltAt: "2026-07-26T00:00:00Z"},
		PublicKeyFile: publicKeyFile,
		StateDir:      stateDir,
		ApplyScript:   applyScript,
		Target:        "linux/test",
		TargetClass:   elf.ELFCLASS64,
		TargetMachine: elf.EM_X86_64,
		Now: func() time.Time {
			return time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
		},
		StartApply: func(script string) error {
			started = append(started, script)
			return nil
		},
	})
	return manager, privateKey, stateDir, &started
}

func signedPackage(t *testing.T, privateKey ed25519.PrivateKey, version, target string, binary []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(binary)
	return signedPackageWithHash(t, privateKey, version, target, binary, hex.EncodeToString(sum[:]))
}

func signedPackageWithHash(t *testing.T, privateKey ed25519.PrivateKey, version, target string, binary []byte, hash string) []byte {
	t.Helper()
	metadata := manifest{
		Format:  1,
		Version: version,
		Target:  target,
		SHA256:  hash,
		BuiltAt: "2026-07-26T00:00:00Z",
	}
	manifestBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(privateKey, manifestBytes)
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, content := range map[string][]byte{
		manifestName:  manifestBytes,
		binaryName:    binary,
		signatureName: []byte(base64.StdEncoding.EncodeToString(signature)),
	} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func executableBytes() []byte {
	content := make([]byte, 64)
	copy(content[:4], []byte{0x7f, 'E', 'L', 'F'})
	content[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	content[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	content[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	binary.LittleEndian.PutUint16(content[16:18], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(content[18:20], uint16(elf.EM_X86_64))
	binary.LittleEndian.PutUint32(content[20:24], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint16(content[52:54], 64)
	binary.LittleEndian.PutUint16(content[54:56], 56)
	binary.LittleEndian.PutUint16(content[58:60], 64)
	return content
}

func stringsOfByte(value string, count int) string {
	var result bytes.Buffer
	for range count {
		result.WriteString(value)
	}
	return result.String()
}
