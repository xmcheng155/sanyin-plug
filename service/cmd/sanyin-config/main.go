package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	assets "sanyin.local/config"
	"sanyin.local/config/service/internal/adapter"
	"sanyin.local/config/service/internal/api"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8787", "HTTP 监听地址")
	mode := flag.String("mode", "mock", "运行模式：mock、adb 或 device")
	adbPath := flag.String("adb", "", "ADB 可执行文件路径（adb 模式）")
	serial := flag.String("serial", "", "ADB 设备序列号（adb 模式）")
	passwordFile := flag.String("password-file", "", "可选的 HTTP Basic Auth 密码文件；默认不启用登录认证")
	flag.Parse()

	provider, err := buildProvider(*mode, *adbPath, *serial)
	if err != nil {
		log.Fatal(err)
	}
	apiHandler := api.NewHandler(provider)
	mux := http.NewServeMux()
	mux.Handle(api.BasePath+"/", apiHandler)
	mux.Handle(api.BasePath, apiHandler)
	mux.Handle("/", http.FileServer(http.FS(assets.WebFS())))
	handler := http.Handler(mux)
	handler = csrfProtection(handler)
	handler, err = withOptionalBasicAuth(handler, *passwordFile)
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:              *listen,
		Handler:           securityHeaders(handler),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("三音本地配置服务（%s）已启动：http://%s", provider.Environment(), *listen)
	if *passwordFile == "" {
		log.Printf("网页登录认证未启用；同一局域网内可直接访问")
	} else {
		log.Printf("网页登录认证已启用")
	}
	if provider.Environment() == "mock" {
		log.Printf("当前不会连接设备或调用 AirPlay 恢复服务")
	} else {
		log.Printf("当前已连接真实设备；网页操作会明确要求二次确认并进行结果验收")
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func withOptionalBasicAuth(next http.Handler, passwordFile string) (http.Handler, error) {
	if passwordFile == "" {
		return next, nil
	}
	password, err := readPassword(passwordFile)
	if err != nil {
		return nil, err
	}
	return basicAuth(next, "admin", password), nil
}

func readPassword(filename string) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("读取密码文件失败: %w", err)
	}
	password := strings.TrimSpace(string(content))
	if len(password) < 12 {
		return "", errors.New("HTTP 密码至少需要 12 个字符")
	}
	return password, nil
}

func basicAuth(next http.Handler, username, password string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providedUser, providedPassword, ok := r.BasicAuth()
		userMatch := subtle.ConstantTimeCompare([]byte(providedUser), []byte(username)) == 1
		passwordMatch := subtle.ConstantTimeCompare([]byte(providedPassword), []byte(password)) == 1
		if !ok || !userMatch || !passwordMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="Sanyin Local Control", charset="UTF-8"`)
			http.Error(w, "需要登录", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func csrfProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		origin, err := url.Parse(r.Header.Get("Origin"))
		if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host != r.Host || r.Header.Get("X-Sanyin-CSRF") != "1" {
			http.Error(w, "拒绝跨站写入请求", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func init() {
	flag.Usage = func() {
		fmt.Println("用法：go run ./service/cmd/sanyin-config [-mode mock|adb|device] [-listen 127.0.0.1:8787]")
		flag.PrintDefaults()
	}
}

func buildProvider(mode, adbPath, serial string) (adapter.Provider, error) {
	switch mode {
	case "mock":
		return adapter.NewMockProvider(), nil
	case "device":
		return adapter.NewRealProvider(adapter.NewLocalShellRunner()), nil
	case "adb":
		resolvedADB, err := findADB(adbPath)
		if err != nil {
			return nil, err
		}
		resolvedSerial, err := findDevice(resolvedADB, serial)
		if err != nil {
			return nil, err
		}
		provider := adapter.NewRealProvider(adapter.NewADBShellRunner(resolvedADB, resolvedSerial))
		device, _ := provider.ForScenario(provider.DefaultScenario())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := device.Device(ctx); err != nil {
			return nil, fmt.Errorf("读取真实设备失败: %w", err)
		}
		log.Printf("ADB 设备：%s", resolvedSerial)
		return provider, nil
	default:
		return nil, fmt.Errorf("未知运行模式 %q；可选值为 mock、adb、device", mode)
	}
}

func findADB(configured string) (string, error) {
	candidates := []string{configured, os.Getenv("ADB"), filepath.Join(".tools", "platform-tools", "adb")}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		path, err := filepath.Abs(candidate)
		if err == nil {
			if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() && info.Mode()&0111 != 0 {
				return path, nil
			}
		}
	}
	if path, err := exec.LookPath("adb"); err == nil {
		return path, nil
	}
	return "", errors.New("未找到 adb；请运行 tools/get_adb_macos.sh 或使用 -adb 指定")
}

func findDevice(adbPath, configured string) (string, error) {
	output, err := exec.Command(adbPath, "devices").Output()
	if err != nil {
		return "", fmt.Errorf("读取 ADB 设备列表失败: %w", err)
	}
	devices := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(output), "\r", ""), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "device" {
			devices = append(devices, fields[0])
		}
	}
	if configured != "" {
		for _, device := range devices {
			if device == configured {
				return device, nil
			}
		}
		return "", fmt.Errorf("ADB 设备未在线: %s", configured)
	}
	for _, device := range devices {
		if strings.HasPrefix(device, "netease_") {
			return device, nil
		}
	}
	if len(devices) == 1 {
		return devices[0], nil
	}
	if len(devices) == 0 {
		return "", errors.New("没有发现处于 device 状态的 ADB 设备")
	}
	return "", errors.New("检测到多个 ADB 设备，请使用 -serial 明确指定")
}
